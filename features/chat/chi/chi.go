// SCOPE:feature — Chi chat slice (conversation demo)
//
// Same routes, wire and components as the framework handlers: index page,
// ask-to-queue with thinking ack, answer stream, background worker. History
// persists through the MessageStore seam (memory in tests and demos) and
// the model through the Responder seam (stub in tests). The chi slice runs
// its own queue instance so its workers never compete with the other
// stack's for the same jobs.
package chi

import (
	"encoding/json"
	"fmt"
	"log/slog"
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
	"github.com/calionauta/gogogo/internal/queue"
)

// Signal names for the Ask panel (one spelling per signal, so painters
// and readers never drift).
const (
	signalChatThinking = "chat_thinking"
	signalChatError    = "chat_error"
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
}

// NewHandler builds the chi chat handler. Nil deps panic: wire errors
// surface at boot, not per request.
func NewHandler(
	cfg *config.Config, a *auth.Auth, q *queue.Queue, hub *queue.SSEHub,
	st store.MessageStore, responder store.Responder,
) *Handler {
	if cfg == nil || a == nil || q == nil || hub == nil || st == nil || responder == nil {
		panic("chat chi: NewHandler with nil dependency")
	}
	return &Handler{cfg: cfg, auth: a, q: q, hub: hub, store: st, responder: responder}
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
	email := sess.User.Email
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = chatcomponents.ChatIndex(email, h.cfg.BuildLabel, h.cfg.BuildCommit).Render(r.Context(), w)
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
		slog.Error("chat: marshal ask job", "error", err)
		writeText(w, http.StatusInternalServerError, "enqueue failed")
		return
	}
	if err := h.q.Enqueue(r.Context(), mustJSON(queue.Job{
		Type: "chat_ask", ClientID: r.URL.Query().Get("clientID"), Payload: payload,
	})); err != nil {
		slog.Error("chat: enqueue failed", "error", err)
		writeText(w, http.StatusInternalServerError, "enqueue failed")
		return
	}
	sse := sdk.NewSSE(w, r)
	if err := dshelpers.MergeSignals(sse, map[string]any{
		signalChatThinking: true,
		signalChatError:    "",
	}); err != nil {
		slog.Error("chat: ask ack failed", "error", err)
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
		slog.Error("chat: stream handshake failed", "error", err)
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
				slog.Error("chat: stream dispatch failed", "error", err)
			}
		}
	}
}

// surface as write errors, checked per call.
// dispatchStreamMessage routes one hub envelope: "chat" releases the
// spinner and appends the assistant bubble into #chat-messages; anything
// else is ignored (a foreign producer sharing nothing must still not
// break this stream).
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
		return dshelpers.RenderAndPatch(sse, chatcomponents.ChatList(nil, false, p.Error),
			sdk.WithSelector("#chat-messages"))
	}
	comps := renderDirectives(p.Directives)
	view := store.MessageView{Role: roleAssistant, Text: p.Text, Components: comps}
	return dshelpers.RenderAndPatch(sse, chatcomponents.ChatList([]store.MessageView{view}, false, ""),
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

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Error("chat: marshal envelope", "error", err)
		return []byte("{}")
	}
	return b
}
