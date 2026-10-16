// SCOPE:feature — HTTP chat slice (conversation demo)
//
// Same routes, wire and components as the framework handlers: index page,
// ask-to-queue with thinking ack, answer stream, background worker. History
// persists through the MessageStore seam (memory in tests and demos) and
// the model through the Responder seam (stub in tests). The chi slice runs
// its own queue instance so its workers never compete with the other
// stack's for the same jobs.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/config"
	chatcomponents "github.com/calionauta/gogogo/features/chat/components"
	"github.com/calionauta/gogogo/features/chat/store"
	comp "github.com/calionauta/gogogo/features/genui/components"
	"github.com/calionauta/gogogo/internal/auth"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/queue"
)

// Signal names for the Ask panel (one spelling per signal, so painters
// and readers never drift).
const (
	signalChatThinking = "chat_thinking"
	signalChatError    = "chat_error"
	signalChatPrompt   = "chatPrompt"
)

// chatEnvelope is the worker → tab envelope: answer text, rendered
// directives, or a model error. The stream dispatcher routes on Type.
type chatEnvelope struct {
	Text       string `json:"text,omitempty"`
	Directives any    `json:"directives,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Handler serves the Ask demo on chi: page, enqueue, stream, worker.
type Handler struct {
	cfg       *config.Config
	auth      *auth.Auth
	q         *queue.Queue
	hub       *queue.SSEHub
	store     store.MessageStore
	responder store.Responder
	// panels are the registered system components: each fills its own props
	// from the app's store at answer time (the catalog's hydration seam).
	// Empty drops such a panel rather than rendering it empty, which is the
	// right answer for a build that wired none.
	panels []comp.PanelProvider
}

// NewHandler builds the chi chat handler. Nil deps panic: wire errors
// surface at boot, not per request.
func NewHandler(
	cfg *config.Config, a *auth.Auth, q *queue.Queue, hub *queue.SSEHub,
	st store.MessageStore, responder store.Responder, panels []comp.PanelProvider,
) *Handler {
	if cfg == nil || a == nil || q == nil || hub == nil || st == nil || responder == nil {
		panic("chat chi: NewHandler with nil dependency")
	}
	return &Handler{cfg: cfg, auth: a, q: q, hub: hub, store: st, responder: responder, panels: panels}
}

// Routes returns the chi router with the chat pages and API.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/chat", h.handleIndex)
	r.Post("/api/chat/ask", h.handleAsk)
	r.Get("/api/chat/stream", h.handleStream)
	return r
}

// RegisterHandlers wires the background job into the queue registry.
// Call before StartWorkers.
func (h *Handler) RegisterHandlers(reg *queue.HandlerRegistry) {
	reg.Register("chat_ask", h.handleChatJob)
}

func writeText(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(msg))
}

// session returns the validated session or nil: pages redirect on nil,
// API handlers fail closed without it.
func (h *Handler) session(r *http.Request) *auth.ValidatedSession {
	sess, err := h.auth.Session(r)
	if err != nil {
		return nil
	}
	return sess
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	// History is server-rendered: the store is the conversation's source
	// of truth, and the live patches below must land on the same shape a
	// reload rebuilds.
	msgs, err := h.store.List(r.Context(), sess.User.ID)
	if err != nil {
		obsArgs := append(observability.WithComponent("chat", "feature"), "error", err)
		observability.Error(r.Context(), "chat: load history failed", obsArgs...)
		msgs = nil
	}
	email := sess.User.Email
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = chatcomponents.ChatIndex(email, sess.Role, h.cfg.BuildLabel, h.cfg.BuildCommit,
		messageViews(r.Context(), msgs)).Render(r.Context(), w)
}

// messageViews maps stored records to render views, re-validating the
// directives through the catalog on the way: history re-renders through
// the same components a live answer used, never raw model markup.
func messageViews(ctx context.Context, msgs []store.Message) []store.MessageView {
	out := make([]store.MessageView, 0, len(msgs))
	for _, m := range msgs {
		//nolint:contextcheck // pure CPU (catalog render); ctx is here for the log.
		comps, err := comp.RenderAll(m.Components)
		if err != nil {
			obsArgs := append(observability.WithComponent("chat", "feature"), "error", err)
			observability.Error(ctx, "chat: render stored answer failed", obsArgs...)
			comps = nil
		}
		out = append(out, store.MessageView{Role: m.Role, Text: m.Text, Components: comps})
	}
	return out
}

func (h *Handler) handleAsk(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if ferr := r.ParseForm(); ferr != nil {
		writeText(w, http.StatusBadRequest, "invalid form")
		return
	}
	prompt := r.FormValue("prompt")
	if prompt == "" {
		writeText(w, http.StatusBadRequest, "prompt required")
		return
	}
	payload, err := json.Marshal(map[string]string{
		"prompt": prompt, "userId": sess.User.ID, "idem": uuid.NewString(),
	})
	if err != nil {
		obsArgs := append(observability.WithComponent("chat", "feature"), "error", err)
		observability.Error(r.Context(), "chat: marshal ask job", obsArgs...)
		writeText(w, http.StatusInternalServerError, "enqueue failed")
		return
	}
	if err := h.q.Enqueue(r.Context(), mustJSON(r.Context(), queue.Job{
		Type: "chat_ask", ClientID: r.URL.Query().Get("clientID"), Payload: payload,
	})); err != nil {
		obsArgs := append(observability.WithComponent("chat", "feature"), "error", err)
		observability.Error(r.Context(), "chat: enqueue failed", obsArgs...)
		writeText(w, http.StatusInternalServerError, "enqueue failed")
		return
	}
	sse := sdk.NewSSE(w, r)
	if err := dshelpers.MergeSignals(sse, map[string]any{
		signalChatThinking: true,
		signalChatError:    "",
		signalChatPrompt:   "",
	}); err != nil {
		obsArgs := append(observability.WithComponent("chat", "feature"), "error", err)
		observability.Error(r.Context(), "chat: ask ack failed", obsArgs...)
		return
	}
	// Echo the prompt as the user's own bubble now, on the ack: the answer
	// arrives later from the worker, so without this the composer clears
	// and the question is only in the store until a reload. The patch
	// APPENDS a thread fragment — the empty state above leaves through
	// $chat_empty (cleared by the composer submit), never through a second
	// #chat-messages.
	//nolint:contextcheck // ends in datastar-go sinks, which take no context;
	// a disconnect surfaces as a write error.
	if err := dshelpers.RenderAndPatch(sse,
		chatcomponents.ChatThread([]store.MessageView{{Role: roleUser, Text: prompt}}, ""),
		sdk.WithSelector("#chat-messages"), sdk.WithModeAppend()); err != nil {
		obsArgs := append(observability.WithComponent("chat", "feature"), "error", err)
		observability.Error(r.Context(), "chat: prompt echo failed", obsArgs...)
	}
}

func (h *Handler) handleStream(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("clientID")
	if clientID == "" {
		clientID = uuid.NewString()
	}
	sse := sdk.NewSSE(w, r)
	ch := make(chan []byte, config.DefaultClientQueueSize)
	h.hub.Register(clientID, "", ch)
	defer h.hub.UnregisterIfCurrent(clientID, ch)

	if err := dshelpers.MergeSignals(sse, map[string]any{
		signalChatThinking: false,
		"clientID":         clientID,
	}); err != nil {
		obsArgs := append(observability.WithComponent("chat", "feature"), "error", err)
		observability.Error(r.Context(), "chat: stream handshake failed", obsArgs...)
		return
	}
	flusher, canFlush := w.(http.Flusher)
	heartbeat := time.NewTicker(config.DefaultSSEHeartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if !canFlush {
				continue
			}
			if _, err := fmt.Fprintf(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case msg := <-ch:
			//nolint:contextcheck // dispatch ends in datastar-go sinks, which
			// take no context; disconnects surface as write errors.
			if err := h.dispatchStreamMessage(sse, msg); err != nil {
				obsArgs := append(observability.WithComponent("chat", "feature"), "error", err)
				observability.Error(r.Context(), "chat: stream dispatch failed", obsArgs...)
			}
		}
	}
}

// surface as write errors, checked per call.
// dispatchStreamMessage routes one hub envelope: "chat" releases the
// spinner and appends one thread fragment (the answer, or the error as
// its own bubble) into #chat-messages; anything else is ignored (a
// foreign producer sharing nothing must still not break this stream).
func (h *Handler) dispatchStreamMessage(sse *sdk.ServerSentEventGenerator, msg []byte) error {
	var job queue.Job
	if err := json.Unmarshal(msg, &job); err != nil {
		return fmt.Errorf("chat: decode stream envelope: %w", err)
	}
	if job.Type != "chat" {
		return nil
	}
	var p chatEnvelope
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return fmt.Errorf("chat: decode chat payload: %w", err)
	}
	if err := dshelpers.MergeSignals(sse, map[string]any{
		signalChatThinking: false,
		signalChatError:    p.Error,
	}); err != nil {
		return err
	}
	if p.Error != "" {
		return dshelpers.RenderAndPatch(sse, chatcomponents.ChatThread(nil, p.Error),
			sdk.WithSelector("#chat-messages"), sdk.WithModeAppend())
	}
	comps := renderDirectives(p.Directives)
	view := store.MessageView{Role: roleAssistant, Text: p.Text, Components: comps}
	return dshelpers.RenderAndPatch(sse, chatcomponents.ChatThread([]store.MessageView{view}, ""),
		sdk.WithSelector("#chat-messages"), sdk.WithModeAppend())
}

// renderDirectives re-validates worker output through the catalog
// before it reaches the DOM: the envelope crossed JSON, so decode via
// ParseDirectives-shaped validation instead of trusting the shape.
func renderDirectives(raw any) []templ.Component {
	if raw == nil {
		return nil
	}
	blob, err := json.Marshal(map[string]any{"components": raw})
	if err != nil {
		return nil
	}
	var envelope struct {
		Components []comp.Directive `json:"components"`
	}
	if decErr := json.Unmarshal(blob, &envelope); decErr != nil {
		return nil
	}
	comps, renderErr := comp.RenderAll(envelope.Components)
	if renderErr != nil {
		return nil
	}
	return comps
}

func mustJSON(ctx context.Context, v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		obsArgs := append(observability.WithComponent("chat", "feature"), "error", err)
		observability.Error(ctx, "chat: marshal envelope", obsArgs...)
		return []byte("{}")
	}
	return b
}
