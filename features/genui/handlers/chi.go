// SCOPE:plugin — HTTP genui slice (Ask demo)
//
// Same routes, wire and components as the framework handlers: index page,
// ask-to-queue with spinner ack, result stream, background worker. The chi
// slice runs its own queue instance so its workers never compete with the
// other stack's for the same jobs.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/config"
	comp "github.com/calionauta/gogogo/features/genui/components"
	"github.com/calionauta/gogogo/internal/auth"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/queue"
)

// Signal names for the Ask panel (one spelling per signal, so painters
// and readers never drift).
const (
	signalGenuiPending = "genui_pending"
	signalGenuiError   = "genui_error"
)

// Handler serves the Ask demo on chi: page, enqueue, stream, worker.
type Handler struct {
	cfg       *config.Config
	auth      *auth.Auth
	q         *queue.Queue
	hub       *queue.SSEHub
	responder comp.Responder
}

// NewHandler builds the chi genui handler. Nil deps panic: wire errors
// surface at boot, not per request.
func NewHandler(
	cfg *config.Config, a *auth.Auth, q *queue.Queue, hub *queue.SSEHub, responder comp.Responder,
) *Handler {
	if cfg == nil || a == nil || q == nil || hub == nil || responder == nil {
		panic("genui chi: NewHandler with nil dependency")
	}
	return &Handler{cfg: cfg, auth: a, q: q, hub: hub, responder: responder}
}

// Routes returns the chi router with the Ask pages and API.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/genui", h.handleIndex)
	r.Post("/api/genui/ask", h.handleAsk)
	r.Get("/api/genui/stream", h.handleStream)
	return r
}

// RegisterHandlers wires the background job into the queue registry.
// Call before StartWorkers.
func (h *Handler) RegisterHandlers(reg *queue.HandlerRegistry) {
	reg.Register("genui_ask", h.handleGenuiJob)
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

// handleIndex serves the Ask page. Login-gated like every app page.
func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	userEmail := sess.User.Email
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	//nolint:contextcheck // generated templ internals take no context beyond
	// the Render call itself, which already receives r.Context().
	_ = comp.GenuiIndex(userEmail, sess.Role, h.cfg.BuildLabel, h.cfg.BuildCommit).Render(r.Context(), w)
}

// handleAsk validates and enqueues an Ask job, acking with a spinner
// signal on the per-request SSE response. The model round-trip happens
// in the worker (never inline); the result lands on /api/genui/stream.
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
	payload, err := json.Marshal(map[string]string{"prompt": prompt, "userId": sess.User.ID})
	if err != nil {
		obsArgs := append(observability.WithComponent("genui", "feature"), "error", err)
		observability.Error(r.Context(), "genui: marshal ask job", obsArgs...)
		writeText(w, http.StatusInternalServerError, "enqueue failed")
		return
	}
	if err := h.q.Enqueue(r.Context(), mustJSON(r.Context(), queue.Job{
		Type: "genui_ask", ClientID: r.URL.Query().Get("clientID"), Payload: payload,
	})); err != nil {
		obsArgs := append(observability.WithComponent("genui", "feature"), "error", err)
		observability.Error(r.Context(), "genui: enqueue failed", obsArgs...)
		writeText(w, http.StatusInternalServerError, "enqueue failed")
		return
	}
	sse := sdk.NewSSE(w, r)
	if err := dshelpers.MergeSignals(sse, map[string]any{
		signalGenuiPending: true,
		signalGenuiError:   "",
		// $genuiPrompt is NOT written here: it is bound two-way to the composer,
		// so the box owns it and a response that clears it can wipe text typed
		// after submit. The client clears it on success.
	}); err != nil {
		obsArgs := append(observability.WithComponent("genui", "feature"), "error", err)
		observability.Error(r.Context(), "genui: ask ack failed", obsArgs...)
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
		signalGenuiPending: false,
		"clientID":         clientID,
	}); err != nil {
		obsArgs := append(observability.WithComponent("genui", "feature"), "error", err)
		observability.Error(r.Context(), "genui: stream handshake failed", obsArgs...)
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
				obsArgs := append(observability.WithComponent("genui", "feature"), "error", err)
				observability.Error(r.Context(), "genui: stream dispatch failed", obsArgs...)
			}
		}
	}
}

func mustJSON(ctx context.Context, v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		obsArgs := append(observability.WithComponent("genui", "feature"), "error", err)
		observability.Warn(ctx, "genui: marshal job", obsArgs...)
		return nil
	}
	return b
}
