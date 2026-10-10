// SCOPE:layer=feature,removal=feature — chat HTTP routes (page, ask, stream)
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/google/uuid"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/auth"
	"github.com/calionauta/gogogo/features/genui"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	"github.com/calionauta/gogogo/internal/queue"
)

// Signal names for the chat panel (one spelling per signal, so
// painters and readers never drift — same rule as every signal file).
const (
	signalChatThinking = "chat_thinking"
	signalChatError    = "chat_error"
)

// New constructs a Handler. Used by router wiring and tests (tests set
// hub/store/responder directly and leave cfg/q nil — HTTP paths only).
func New(cfg *config.Config, q *queue.Queue, hub *queue.SSEHub, store MessageStore, responder Responder) *Handler {
	return &Handler{cfg: cfg, q: q, hub: hub, store: store, responder: responder}
}

// RegisterRoutes wires the HTTP routes on a PocketBase serve event.
func (h *Handler) RegisterRoutes(se *core.ServeEvent) {
	h.RegisterRoutesOn(se.Router)
}

// RegisterRoutesOn wires the same routes on a raw router (tests).
func (h *Handler) RegisterRoutesOn(r *router.Router[*core.RequestEvent]) {
	r.GET("/chat", h.handleIndex)
	r.POST("/api/chat/ask", h.handleAsk)
	r.GET("/api/chat/stream", h.handleStream)
}

// RegisterHandlers wires the background job into the queue registry.
// Call before StartWorkers.
func (h *Handler) RegisterHandlers(reg *queue.HandlerRegistry) {
	reg.Register("chat_ask", h.handleChatJob)
}

// handleIndex serves the conversation page. Login-gated like every app
// page; history renders server-side so a reload never loses context.
func (h *Handler) handleIndex(c *core.RequestEvent) error {
	if err := auth.RequireAuthOrRedirect(c); err != nil {
		return err
	}
	email := ""
	if c.Auth != nil {
		email = c.Auth.Email()
	}
	return ChatIndex(email, h.cfg.BuildLabel, h.cfg.BuildCommit).Render(c.Request.Context(), c.Response)
}

// handleAsk validates and enqueues a chat job, acking with a thinking
// signal on the per-request SSE response. The model round-trip happens
// in the worker (never inline); the answer lands on /api/chat/stream.
func (h *Handler) handleAsk(c *core.RequestEvent) error {
	if err := auth.LoadAppAuth(c); err != nil {
		slog.Debug("chat: ask auth load", "error", err)
	}
	if c.Auth == nil {
		return c.Redirect(http.StatusSeeOther, "/login")
	}
	if err := c.Request.ParseForm(); err != nil {
		return c.String(http.StatusBadRequest, "invalid form")
	}
	prompt := c.Request.FormValue("prompt")
	if prompt == "" {
		return c.String(http.StatusBadRequest, "prompt required")
	}
	payload, err := json.Marshal(map[string]string{
		"prompt": prompt, "userId": c.Auth.Id, "idem": uuid.NewString(),
	})
	if err != nil {
		slog.Error("chat: marshal ask job", "error", err)
		return c.String(http.StatusInternalServerError, "enqueue failed")
	}
	if err := h.q.Enqueue(context.Background(), mustJSON(queue.Job{
		Type: "chat_ask", ClientID: c.Request.URL.Query().Get("clientID"), Payload: payload,
	})); err != nil {
		slog.Error("chat: enqueue failed", "error", err)
		return c.String(http.StatusInternalServerError, "enqueue failed")
	}
	sse := sdk.NewSSE(c.Response, c.Request)
	return dshelpers.MergeSignals(sse, map[string]any{
		signalChatThinking: true,
		signalChatError:    "",
	})
}

// handleStream opens the answer stream for one tab: register, initial
// signals, then pump hub envelopes through the dispatcher until the
// client goes away. Heartbeat write forces Go to notice dead clients
// (same reason as every other stream: a parked handler would leak).
func (h *Handler) handleStream(c *core.RequestEvent) error {
	if err := auth.LoadAppAuth(c); err != nil {
		slog.Debug("chat: stream auth load", "error", err)
	}
	clientID := c.Request.URL.Query().Get("clientID")
	if clientID == "" {
		clientID = uuid.NewString()
	}
	sse := sdk.NewSSE(c.Response, c.Request)
	ch := make(chan []byte, config.DefaultClientQueueSize)
	h.hub.Register(clientID, "", ch)
	defer h.hub.UnregisterIfCurrent(clientID, ch)

	if err := dshelpers.MergeSignals(sse, map[string]any{
		signalChatThinking: false,
		"clientID":         clientID,
	}); err != nil {
		return err
	}
	flusher, canFlush := c.Response.(http.Flusher)
	heartbeat := time.NewTicker(config.DefaultSSEHeartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return nil
		case <-heartbeat.C:
			if !canFlush {
				continue
			}
			if _, err := fmt.Fprintf(c.Response, ": heartbeat\n\n"); err != nil {
				return nil
			}
			flusher.Flush()
		case msg := <-ch:
			if err := h.dispatchStreamMessage(sse, msg); err != nil {
				slog.Warn("chat: dispatch failed", "error", err)
			}
		}
	}
}

// dispatchStreamMessage routes one hub envelope: "chat" releases the
// spinner and appends the assistant bubble (text plus rendered catalog
// components) into #chat-messages; anything else is ignored (a foreign
// producer sharing nothing must still not break this stream).
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
		return dshelpers.RenderAndPatch(sse, ChatList(nil, false, p.Error),
			sdk.WithSelector("#chat-messages"))
	}
	comps := renderDirectives(p.Directives)
	view := MessageView{Role: roleAssistant, Text: p.Text, Components: comps}
	return dshelpers.RenderAndPatch(sse, ChatList([]MessageView{view}, false, ""),
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
	// The worker sends Directive structs; re-decode them as raw model
	// output would decode — one validation path for both directions.
	var envelope struct {
		Components []genui.Directive `json:"components"`
	}
	if decErr := json.Unmarshal(blob, &envelope); decErr != nil {
		return nil
	}
	comps, renderErr := genui.RenderAll(envelope.Components)
	if renderErr != nil {
		return nil
	}
	return comps
}
