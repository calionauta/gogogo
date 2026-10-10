// SCOPE:layer=feature,removal=feature — gogen-ui Ask demo (reference implementation)
package genui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/auth"
	ic "github.com/calionauta/gogogo/internal/components"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	"github.com/calionauta/gogogo/internal/llm"
	"github.com/calionauta/gogogo/internal/queue"
)

// Responder is the model leg behind Ask: production passes an llm
// adapter, tests pass a stub. The queue worker — not the HTTP handler —
// calls it, because an LLM round-trip never runs inline (>50ms rule).
type Responder interface {
	Respond(ctx context.Context, prompt string) (string, error)
}

// llmResponder asks a real model for catalog directives.
type llmResponder struct {
	client *llm.Client
}

func (r llmResponder) Respond(ctx context.Context, prompt string) (string, error) {
	return r.client.Chat(ctx, PromptForCatalog()+"\nUser request: "+prompt)
}

// demoResponder is the keyless showcase: deterministic directives
// exercising every catalog component, labeled as demo data. Same
// role as SIMULATE_LLM in todo — the feature works with zero keys.
type demoResponder struct{}

const demoAnswer = `{"components":[` +
	`{"type":"text_note","props":{"text":"Demo answer — connect GOAI_API_KEY for live model output."}},` +
	`{"type":"plan_cards","props":{"plans":[` +
	`{"title":"Ship the Ask panel","detail":"wire a real prompt through this same path"},` +
	`{"title":"Grow the catalog","detail":"one registry entry per new component"}]}},` +
	`{"type":"data_table","props":{"headers":["Component","Renders"],` +
	`"rows":[["text_note","prose card"],["plan_cards","apply buttons"],["data_table","this table"]]}}]}`

func (demoResponder) Respond(_ context.Context, _ string) (string, error) {
	return demoAnswer, nil
}

type errResponder struct{ err error }

func (e errResponder) Respond(_ context.Context, _ string) (string, error) {
	return "", e.err
}

// NewResponder picks the model leg: real key wins, simulated key gets
// the deterministic demo, neither is a fail-closed error (the worker
// releases the spinner through the error result either way).
func NewResponder(real, simulated *llm.Client) Responder {
	if real != nil && real.Configured() {
		return llmResponder{client: real}
	}
	if simulated != nil && simulated.Configured() {
		return demoResponder{}
	}
	return errResponder{err: errors.New("genui: no LLM configured (GOAI_API_KEY or SIMULATE_LLM=true)")}
}

// Signal names for the Ask panel (mirrors features/todo signal_keys.go:
// one spelling per signal, so painters and readers never drift).
const (
	signalGenuiPending = "genui_pending"
	signalGenuiError   = "genui_error"
)

// Handler serves the Ask demo: page, enqueue, stream, worker.
type Handler struct {
	cfg       *config.Config
	q         *queue.Queue
	hub       *queue.SSEHub
	responder Responder
}

// New constructs a Handler. Used by router wiring and tests.
func New(cfg *config.Config, q *queue.Queue, hub *queue.SSEHub, responder Responder) *Handler {
	return &Handler{cfg: cfg, q: q, hub: hub, responder: responder}
}

// RegisterRoutes wires the HTTP routes on a PocketBase serve event.
func (h *Handler) RegisterRoutes(se *core.ServeEvent) {
	h.RegisterRoutesOn(se.Router)
}

// RegisterRoutesOn wires the same routes on a raw router (tests).
func (h *Handler) RegisterRoutesOn(r *router.Router[*core.RequestEvent]) {
	r.GET("/genui", h.handleIndex)
	r.POST("/api/genui/ask", h.handleAsk)
	r.GET("/api/genui/stream", h.handleStream)
}

// RegisterHandlers wires the background job into the queue registry.
// Call before StartWorkers.
func (h *Handler) RegisterHandlers(reg *queue.HandlerRegistry) {
	reg.Register("genui_ask", h.handleGenuiJob)
}

// handleIndex serves the Ask page. Login-gated like every app page.
func (h *Handler) handleIndex(c *core.RequestEvent) error {
	if err := auth.RequireAuthOrRedirect(c); err != nil {
		return err
	}
	userEmail := ""
	if c.Auth != nil {
		userEmail = c.Auth.Email()
	}
	return GenuiIndex(userEmail, h.cfg.BuildLabel, h.cfg.BuildCommit).Render(c.Request.Context(), c.Response)
}

// handleAsk validates and enqueues an Ask job, acking with a spinner
// signal on the per-request SSE response. The model round-trip happens
// in the worker (never inline); the result lands on /api/genui/stream.
func (h *Handler) handleAsk(c *core.RequestEvent) error {
	if err := auth.LoadAppAuth(c); err != nil {
		slog.Debug("genui: ask auth load", "error", err)
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
	payload, err := json.Marshal(map[string]string{"prompt": prompt, "userId": c.Auth.Id})
	if err != nil {
		slog.Error("genui: marshal ask job", "error", err)
		return c.String(http.StatusInternalServerError, "enqueue failed")
	}
	if err := h.q.Enqueue(context.Background(), mustJSON(queue.Job{
		Type: "genui_ask", ClientID: c.Request.URL.Query().Get("clientID"), Payload: payload,
	})); err != nil {
		slog.Error("genui: enqueue failed", "error", err)
		return c.String(http.StatusInternalServerError, "enqueue failed")
	}
	sse := sdk.NewSSE(c.Response, c.Request)
	return dshelpers.MergeSignals(sse, map[string]any{
		signalGenuiPending: true,
		signalGenuiError:   "",
	})
}

// handleStream opens the result stream for one tab: register, initial
// signals, then pump hub envelopes through the dispatcher until the
// client goes away. Heartbeat write forces Go to notice dead clients
// (same reason as the todo stream: a parked handler would leak).
func (h *Handler) handleStream(c *core.RequestEvent) error {
	if err := auth.LoadAppAuth(c); err != nil {
		slog.Debug("genui: stream auth load", "error", err)
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
		signalGenuiPending: false,
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
				slog.Warn("genui: dispatch failed", "error", err)
			}
		}
	}
}

// genuiPayload is the worker → tab envelope: rendered directives or an
// error, plus the spinner release. One shape for success and failure so
// the dispatcher has a single branch.
type genuiPayload struct {
	Directives []Directive `json:"directives,omitempty"`
	Error      string      `json:"error,omitempty"`
}

// handleGenuiJob runs the model leg in the worker pool: ask, parse
// against the catalog, fan the result to the originator. Errors return
// (worker retries) AND surface immediately (tab gets an error result +
// toast on the first attempt — no silent spinner).
func (h *Handler) handleGenuiJob(ctx context.Context, hub *queue.SSEHub, job queue.Job) error {
	var p struct {
		Prompt string `json:"prompt"`
		UserID string `json:"userId"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return fmt.Errorf("genui: decode ask payload: %w", err)
	}
	raw, err := h.responder.Respond(ctx, p.Prompt)
	if err != nil {
		h.sendResult(hub, job.ClientID, nil, "Ask failed: model unavailable")
		h.sendToast(hub, job.ClientID, "Ask failed: model unavailable", "error")
		return fmt.Errorf("genui: respond: %w", err)
	}
	//nolint:contextcheck // pure CPU constructors (no I/O to cancel); ctx enters at component Render instead
	dirs, err := ParseDirectives(raw)
	if err != nil {
		h.sendResult(hub, job.ClientID, nil, "Ask failed: could not shape the answer")
		h.sendToast(hub, job.ClientID, "Ask failed: could not shape the answer", "error")
		return fmt.Errorf("genui: parse directives: %w", err)
	}
	h.sendResult(hub, job.ClientID, dirs, "")
	h.sendToast(hub, job.ClientID, fmt.Sprintf("Rendered %d components", len(dirs)), "success")
	return nil
}

// sendResult delivers the render payload to one tab (or broadcasts when
// the job carries no client).
func (h *Handler) sendResult(hub *queue.SSEHub, clientID string, dirs []Directive, errMsg string) {
	body := mustJSON(queue.Job{Type: "genui", Payload: mustJSON(genuiPayload{Directives: dirs, Error: errMsg})})
	if clientID == "" {
		hub.Broadcast(body)
		return
	}
	hub.Send(clientID, body)
}

// sendToast delivers a toast to one tab (or broadcasts).
func (h *Handler) sendToast(hub *queue.SSEHub, clientID, message, kind string) {
	p := mustJSON(map[string]string{"toastType": kind, "message": message})
	body := mustJSON(queue.Job{Type: "toast", Payload: p})
	if clientID == "" {
		hub.Broadcast(body)
		return
	}
	hub.Send(clientID, body)
}

// dispatchStreamMessage routes one hub envelope to its patch. Two cases:
// "genui" renders components into #genui-result, "toast" appends a
// toast. Anything else is ignored (never an error — a foreign producer
// sharing nothing must still not break this stream).
func (h *Handler) dispatchStreamMessage(sse *sdk.ServerSentEventGenerator, msg []byte) error {
	var job queue.Job
	if err := json.Unmarshal(msg, &job); err != nil {
		return fmt.Errorf("genui: decode stream envelope: %w", err)
	}
	switch job.Type {
	case "genui":
		var p genuiPayload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return fmt.Errorf("genui: decode result payload: %w", err)
		}
		if err := dshelpers.MergeSignals(sse, map[string]any{
			signalGenuiPending: false,
			signalGenuiError:   p.Error,
		}); err != nil {
			return err
		}
		comps, err := RenderAll(p.Directives)
		if err != nil {
			return err
		}
		return dshelpers.RenderAndPatch(sse, GenuiResult(comps, p.Error, p.Error != ""),
			sdk.WithSelector("#genui-result"),
			sdk.WithViewTransitions())
	case "toast":
		var t struct {
			ToastType string `json:"toastType"`
			Message   string `json:"message"`
		}
		if err := json.Unmarshal(job.Payload, &t); err != nil {
			return fmt.Errorf("genui: decode toast payload: %w", err)
		}
		if t.ToastType == "" {
			t.ToastType = "info"
		}
		return genuiToast(sse, t.Message, t.ToastType)
	default:
		return nil
	}
}

// genuiToast appends a toast to the shared toast container (same
// component the todo feature uses — one toast system per app).
func genuiToast(sse *sdk.ServerSentEventGenerator, message, toastType string) error {
	return dshelpers.RenderAndPatch(
		sse,
		ic.Toast(message, toastType, ic.NewToastID()),
		sdk.WithSelectorID("toast-container"),
		sdk.WithModeAppend(),
	)
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Warn("genui: marshal job", "error", err)
		return nil
	}
	return b
}
