// SCOPE:feature — HTTP chat slice (conversation demo)
//
// Same routes, wire and components as the framework handlers: index page,
// question-to-queue with thinking ack, answer stream, background worker. History
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
	"github.com/calionauta/gogogo/internal/demo"
	"github.com/calionauta/gogogo/internal/llm"
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/queue"
)

// Signal names for the Chat panel (one spelling per signal, so painters
// and readers never drift).
const (
	signalChatThinking = "chat_thinking"
	signalChatError    = "chat_error"
	signalChatReady    = "chat_ready"
	signalChatWorking  = "chat_working"
)

// chatEnvelope is the worker → tab envelope: answer text, rendered
// directives, or a model error. The stream dispatcher routes on Type.
type chatEnvelope struct {
	Text       string `json:"text,omitempty"`
	Directives any    `json:"directives,omitempty"`
	Error      string `json:"error,omitempty"`
	// Working is the mid-answer stage ("Loading your data…"): a signal-only
	// envelope, no patch. The dispatcher keeps the spinner and names the
	// stage in the thinking bubble; the terminal envelope clears it.
	Working string `json:"working,omitempty"`
	// IDPrefix is the DOM-id stem this answer's components render under. The
	// worker computes it from the message it just stored (store.IDPrefix), so
	// the live patch and a later reload agree on every id.
	IDPrefix string `json:"idPrefix,omitempty"`
}

// Handler serves the Chat demo on chi: page, enqueue, stream, worker.
type Handler struct {
	cfg       *config.Config
	auth      *auth.Auth
	q         *queue.Queue
	hub       *queue.SSEHub
	store     store.MessageStore
	responder store.Responder
	// defaultSurfaces is the boot vocabulary (GENUI_CHAT_SURFACES, or the
	// Chat default). Per-request surfaces override it; nil (old clients,
	// tests) falls back here.
	defaultSurfaces []comp.Surface
	// modelLeg rebuilds the responder per request. Kept as clients +
	// router (not a responder) because the surfaces ride the job: one
	// shared responder cannot serve two tabs with different toggles.
	// Nil in tests, which inject a stub responder instead.
	realLLM *llm.Client
	simLLM  *llm.Client
	router  comp.Router
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

// SetModelLeg installs the production model leg: the two LLM clients, the
// optional lead-component router, and the boot surface default. The worker
// rebuilds the responder per job from these plus the tab's toggles; tests
// skip this and keep the injected stub responder.
func (h *Handler) SetModelLeg(real, sim *llm.Client, router comp.Router, surfaces []comp.Surface) {
	h.realLLM = real
	h.simLLM = sim
	h.router = router
	h.defaultSurfaces = surfaces
}

// Routes returns the chi router with the chat pages and API.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/chat", h.handleIndex)
	r.Post("/api/chat/ask", h.handleAsk)
	r.Post("/api/chat/clear", h.handleClear)
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
	def := h.defaultSurfaces
	if def == nil {
		def = comp.DefaultSurfacesChat
	}
	_ = chatcomponents.ChatIndex(email, sess.Role, h.cfg.BuildLabel, h.cfg.BuildCommit,
		messageViews(r.Context(), msgs), demo.On(r),
		comp.HasSurfaces(def, comp.SurfaceCatalog),
		comp.HasSurfaces(def, comp.SurfaceCustom),
		comp.HasSurfaces(def, comp.SurfaceNative)).Render(r.Context(), w)
}

// messageViews maps stored records to render views, re-validating the
// directives through the catalog on the way: history re-renders through
// the same components a live answer used, never raw model markup.
//
// Panels REFRESH from the store on the way (the same Fill the worker ran), so a
// reload shows the viewer's CURRENT rows, not the snapshot the answer stored —
// a mutation from another tab lands on the next reload. Rehydrate, not Hydrate:
// the stored directive already carries Fill's output, which the parse-time
// writable gate rejects (see components.Rehydrate). A failed re-fill keeps the
// stored snapshot instead of an honest hole: the rows were real when stored,
// and a transient store error must not blank them.
func messageViews(ctx context.Context, msgs []store.Message) []store.MessageView {
	out := make([]store.MessageView, 0, len(msgs))
	for _, m := range msgs {
		dirs, rerr := comp.Rehydrate(ctx, m.Components, m.Owner)
		if rerr != nil {
			obsArgs := append(observability.WithComponent("chat", "feature"), "error", rerr)
			observability.Warn(ctx, "chat: history re-fill failed, keeping stored panel rows", obsArgs...)
		}
		//nolint:contextcheck // pure CPU (catalog render); ctx is here for the log.
		// Prefix by the message's own idempotency key: a thread keeps every
		// answer on the page, so without a per-message stem the second answer's
		// components would repeat the first one's DOM ids — invalid HTML, and a
		// selector that silently means "the first match".
		comps, err := comp.RenderAllWithPrefix(dirs, store.IDPrefix(m))
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
	surfaces, serr := requestSurfaces(r)
	if serr != nil {
		writeText(w, http.StatusBadRequest, "invalid surfaces")
		return
	}
	payload, err := json.Marshal(chatJob{
		Prompt: prompt, UserID: sess.User.ID, Idem: uuid.NewString(), Surfaces: surfaces,
	})
	if err != nil {
		obsArgs := append(observability.WithComponent("chat", "feature"), "error", err)
		observability.Error(r.Context(), "chat: marshal ask job", obsArgs...)
		writeText(w, http.StatusInternalServerError, "enqueue failed")
		return
	}
	// ACK BEFORE THE JOB, and the ack owns NO thinking-state signal.
	//
	// Same ownership rule as the todo suggest ack (see enqueueSuggest): the
	// worker's result travels on the tab's already-open SSE stream while this
	// response travels back on the POST connection — nothing orders them, and
	// behind a tunnel the response is the slower one. An ack that re-asserted
	// `chat_thinking: true` could therefore land AFTER the worker's terminal
	// patch (`chat_thinking: false`) and re-disable the Send button for good:
	// `data-attr:disabled="!!$chat_thinking"` on a signal that is never cleared
	// again is a dead control, not a stuck spinner.
	//
	// The composer owns the optimistic state (`$chat_thinking = true` on
	// submit, `false` on fetch error), and the worker owns the terminal state.
	sse := sdk.NewSSE(w, r)
	if err := h.q.Enqueue(r.Context(), mustJSON(r.Context(), queue.Job{
		Type: "chat_ask", ClientID: r.URL.Query().Get("clientID"), Payload: payload,
	})); err != nil {
		obsArgs := append(observability.WithComponent("chat", "feature"), "error", err)
		observability.Error(r.Context(), "chat: enqueue failed", obsArgs...)
		// Headers are out, so the failure rides the open stream as a terminal
		// patch — a 500 here would leave the composer disabled with nothing
		// to clear it.
		if merr := dshelpers.MergeSignals(sse, map[string]any{
			signalChatThinking: false,
			signalChatError:    "enqueue failed",
		}); merr != nil {
			obsArgs := append(observability.WithComponent("chat", "feature"), "error", merr)
			observability.Error(r.Context(), "chat: enqueue failure not delivered", obsArgs)
		}
		return
	}
	// The SUCCESS ack writes NO signal at all — it carries the prompt echo and
	// nothing else, the same shape as enqueueSuggest.
	//
	// The composer owns every piece of state here: `$chat_thinking = true` and
	// `$chat_error = ''` on submit, `$chat_thinking = false` on a fetch error.
	// The worker owns the terminal state. An ack that wrote any of them could
	// land AFTER the worker's terminal patch — the POST response and the SSE
	// stream are different connections with no ordering between them, and
	// behind a tunnel the response is slower — and re-disable the Send button
	// for good (`data-attr:disabled="!!$chat_thinking"` on a signal that is
	// never cleared again is a dead control, not a stuck spinner).
	//
	// The echo below is a PATCH, not a signal write, and it appends to
	// #chat-messages — it carries no state signal.
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

func (h *Handler) handleClear(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := h.store.Clear(r.Context(), sess.User.ID); err != nil {
		obsArgs := append(observability.WithComponent("chat", "feature"), "error", err)
		observability.Error(r.Context(), "chat: clear failed", obsArgs...)
		writeText(w, http.StatusInternalServerError, "clear failed")
		return
	}
	// New-chat ack: reset the composer-owned state the submit path sets
	// ($chat_thinking, $chat_error, $chat_working) plus the empty flag, and
	// swap the thread for the empty guidance. Morph (no append mode) replaces
	// #chat-messages with the empty column, so the scroll anchor and hero
	// come back. $chatPrompt is NOT written here: the input owns it (see
	// the ask-ack contract above) and the confirm button clears it
	// client-side before posting.
	sse := sdk.NewSSE(w, r)
	if err := dshelpers.MergeSignals(sse, map[string]any{
		signalChatThinking: false,
		signalChatError:    "",
		signalChatWorking:  "",
		"chat_empty":       true,
	}); err != nil {
		obsArgs := append(observability.WithComponent("chat", "feature"), "error", err)
		observability.Error(r.Context(), "chat: clear signals failed", obsArgs...)
		return
	}
	//nolint:contextcheck // ends in datastar-go sinks, which take no context;
	// a disconnect surfaces as a write error.
	if err := dshelpers.RenderAndPatch(sse,
		chatcomponents.ChatList(nil, ""),
		sdk.WithSelector("#chat-messages")); err != nil {
		obsArgs := append(observability.WithComponent("chat", "feature"), "error", err)
		observability.Error(r.Context(), "chat: clear patch failed", obsArgs...)
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
	if err := dshelpers.AckStreamOpened(sse, clientID); err != nil {
		obsArgs := append(observability.WithComponent("chat", "feature"), "error", err)
		observability.Error(r.Context(), "chat: stream ack failed", obsArgs...)
		return
	}

	authed := h.session(r) != nil
	if err := dshelpers.MergeSignals(sse, map[string]any{
		signalChatThinking: false,
		signalChatReady:    true,
		signalChatWorking:  "",
		"clientID":         clientID,
		"sessionAuthed":    authed,
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
			// The session state rides this stream: the page does not open
			// the dedicated session stream (one long-lived connection per
			// page), so expiry is noticed here.
			if now := h.session(r) != nil; now != authed {
				authed = now
				_ = dshelpers.MergeSignals(sse, map[string]bool{"sessionAuthed": authed})
			}
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
	if p.Working != "" {
		// Working is the mid-answer stage (panel hydration): the spinner
		// stays, the bubble names the stage, and nothing appends yet — the
		// terminal patch below carries the answer. Not a second bubble, so
		// a slow hydration reads as progress, not as a duplicate spinner.
		return dshelpers.MergeSignals(sse, map[string]any{
			signalChatThinking: true,
			signalChatWorking:  p.Working,
		})
	}
	if err := dshelpers.MergeSignals(sse, map[string]any{
		signalChatThinking: false,
		signalChatError:    p.Error,
		signalChatWorking:  "",
		signalChatReady:    true,
	}); err != nil {
		return err
	}
	if p.Error != "" {
		return dshelpers.RenderAndPatch(sse, chatcomponents.ChatThread(nil, p.Error),
			sdk.WithSelector("#chat-messages"), sdk.WithModeAppend())
	}
	comps := renderDirectivesWithPrefix(p.Directives, p.IDPrefix)
	view := store.MessageView{Role: roleAssistant, Text: p.Text, Components: comps}
	return dshelpers.RenderAndPatch(sse, chatcomponents.ChatThread([]store.MessageView{view}, ""),
		sdk.WithSelector("#chat-messages"), sdk.WithModeAppend())
}

// renderDirectives re-validates worker output through the catalog
// before it reaches the DOM: the envelope crossed JSON, so decode via
// ParseDirectives-shaped validation instead of trusting the shape.
func renderDirectives(raw any) []templ.Component {
	return renderDirectivesWithPrefix(raw, "")
}

// renderDirectivesWithPrefix is renderDirectives with an instance-id stem, so
// two answers on one page never mint the same component ids (see
// internal/components/instance.go rule 1).
func renderDirectivesWithPrefix(raw any, idPrefix string) []templ.Component {
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
	comps, renderErr := comp.RenderAllWithPrefix(envelope.Components, idPrefix)
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
