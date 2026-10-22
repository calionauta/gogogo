// SCOPE:feature — HTTP AI suggest slice (queue + LLM, demo path)
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	sdk "github.com/starfederation/datastar-go/datastar"

	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	"github.com/calionauta/gogogo/internal/llm"
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/queue"
)

// handleSuggest prefers the real LLM and falls back to the keyless
// simulated client, so one route serves both paths and the UI never shows
// a dead button. Without either, 503 names the missing configuration
// instead of failing opaquely.
func (h *Handler) handleSuggest(w http.ResponseWriter, r *http.Request) {
	if h.LLM != nil && h.LLM.Configured() {
		h.enqueueSuggest(w, r, signalJobSuggest, r.FormValue("partial"))
		return
	}
	if h.SimulatedLLM != nil && h.SimulatedLLM.Configured() {
		h.enqueueSuggest(w, r, signalJobSuggestSimulated, r.FormValue("partial"))
		return
	}
	writeText(w, http.StatusServiceUnavailable, "LLM not configured: set GOAI_API_KEY or SIMULATE_LLM=true")
}

// handleSuggestSimulated enqueues a "suggest_simulated" job: the same
// queue + retry path against the in-process fake LLM, which scripts
// error → retry → slow response for the demo stepper.
func (h *Handler) handleSuggestSimulated(w http.ResponseWriter, r *http.Request) {
	if h.SimulatedLLM == nil || !h.SimulatedLLM.Configured() {
		writeText(w, http.StatusServiceUnavailable, "simulated LLM not enabled: set SIMULATE_LLM=true")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeText(w, http.StatusBadRequest, "invalid form")
		return
	}
	partial := r.FormValue("partial")
	if partial == "" {
		partial = "Plan my week"
	}
	h.enqueueSuggest(w, r, signalJobSuggestSimulated, partial)
}

// enqueueSuggest builds and enqueues a suggest job, threading the
// originating clientID (for per-tab routing) and the billing subject
// (userID, so the Biller fails closed when billing is on).
func (h *Handler) enqueueSuggest(w http.ResponseWriter, r *http.Request, jobType, partial string) {
	owner, err := h.ownerID(r)
	if err != nil {
		redirectLogin(w, r)
		return
	}
	clientID := r.URL.Query().Get("clientID")
	payload, err := json.Marshal(map[string]string{"partial": partial, "userId": owner})
	if err != nil {
		h.fail(r.Context(), w, "todo: suggest marshal failed", err, "marshal failed")
		return
	}
	job, err := json.Marshal(queue.Job{Type: jobType, ClientID: clientID, Payload: payload})
	if err != nil {
		h.fail(r.Context(), w, "todo: suggest job marshal failed", err, "marshal job failed")
		return
	}
	if h.Queue == nil {
		h.fail(r.Context(), w, "todo: suggest queue missing", errNoQueue, "enqueue failed")
		return
	}
	// ACK FIRST, then enqueue: the worker streams its terminal patch over the
	// tab's SSE stream the instant the job is accepted.
	//
	// The ack deliberately merges NOTHING. Every signal this endpoint could
	// usefully reset (`suggestions`, `suggestErr`, `aiStep`, `aiPending`) is
	// owned by the worker's terminal patch, and the POST response can reach the
	// browser AFTER that patch (it travels back over the same slow link the
	// stream already holds open — Cloudflare tunnel, or a Datastar retry). An
	// ack that re-asserted `aiPending: true` and `suggestions: []` produced the
	// stuck "Asking AI…" spinner on a completed run: aiStep=3 from the result,
	// aiPending=true from the ack, suggestions erased. Own the optimistic state
	// in the click handler, own the terminal state in the worker, and let the
	// ack mean only "accepted".
	sse := sdk.NewSSE(w, r)
	if err := h.Queue.Enqueue(r.Context(), job); err != nil {
		// The SSE headers are out, so the failure rides the open stream as a
		// terminal patch — a 500 here would leave the optimistic spinner up
		// with nothing left to clear it.
		if merr := dshelpers.MergeSignals(sse, map[string]any{
			signalSuggestErr:  "enqueue failed",
			signalAiPending:   false,
			signalAiPhase:     aiPhaseError,
			signalSuggestions: []string{},
		}); merr != nil {
			obsArgs := append(observability.WithComponent("todo", "feature"), "error", merr)
			observability.Error(r.Context(), "todo: suggest enqueue failure not delivered", obsArgs)
		}
		obsArgs := append(observability.WithComponent("todo", "feature"), "error", err)
		observability.Error(r.Context(), "todo: suggest enqueue failed", obsArgs...)
	}
}

// errNoQueue is the fail-closed error when suggest/retry routes run
// without a wired queue (StartChi always wires one; tests choose).
var errNoQueue = errQueueMissing()

func errQueueMissing() error {
	return errors.New("todo: no queue wired")
}

// RegisterSuggestHandlers wires the suggest workers into the queue
// registry. Call before StartWorkers, on the same queue the routes
// enqueue to.
func (h *Handler) RegisterSuggestHandlers(reg *queue.HandlerRegistry) {
	reg.Register(signalJobSuggest, h.handleSuggestJob)
	reg.Register(signalJobSuggestSimulated, h.handleSuggestJob)
}

// handleSuggestJob runs inside the queue's retry: transient failures
// stream per-attempt feedback, success pushes suggestions over SSE, and
// failure pushes an error result and returns the error so the worker
// retries (visible feedback, not silent loss).
func (h *Handler) handleSuggestJob(ctx context.Context, hub *queue.SSEHub, job queue.Job) error {
	var p struct {
		Partial string `json:"partial"`
		UserID  string `json:"userId"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return fmt.Errorf("decode suggest payload: %w", err)
	}

	// Stamp the billing subject into the ctx so the llm Client's optional
	// Biller attributes this call. Empty userID + a set meter fails
	// closed inside ChatSuggest.
	ctx = llm.WithUserID(ctx, p.UserID)

	client := h.LLM
	if job.Type == signalJobSuggestSimulated {
		client = h.SimulatedLLM
	}
	if !llmConfigured(h, job) {
		return h.sendUnconfigured(ctx, hub, job, client)
	}

	suggestions, err := client.ChatSuggest(ctx, p.Partial)
	result := map[string]any{
		signalSuggestions: []string{},
		signalSuggestErr:  "",
		signalAiPending:   false,
	}
	if err != nil {
		// Provider auth failures carry the key↔base mismatch hint
		// (llm.WithAuthHint), same as the Ask/Chat workers.
		result[signalSuggestErr] = "AI suggest failed: " + llm.WithAuthHint(err).Error()
		obsArgs := append(observability.WithComponent("todo", "feature"), "job", job.Type, "error", err)
		observability.Error(ctx, "todo: suggest job failed", obsArgs...)
	} else {
		result[signalSuggestions] = suggestions
	}

	body, marshalErr := json.Marshal(queue.Job{Type: jobTypeSuggestResult, Payload: mustJSON(ctx, result)})
	if marshalErr != nil {
		return fmt.Errorf("marshal suggest result: %w", marshalErr)
	}
	if job.ClientID != "" {
		hub.Send(job.ClientID, body)
		return err // worker retries on failure (visible SSE feedback)
	}
	//nolint:contextcheck // SSEHub.Broadcast takes no context; worker
	// cancellation flows through the surrounding ctx select.
	hub.Broadcast(body)
	return err
}

// llmConfigured reports whether the job's leg has a usable LLM client. Split
// out so the worker reads as "guard, then work" instead of a nested delivery
// inside an error arm.
func llmConfigured(h *Handler, job queue.Job) bool {
	client := h.LLM
	if job.Type == signalJobSuggestSimulated {
		client = h.SimulatedLLM
	}
	return client != nil && client.Configured()
}

// sendUnconfigured delivers the "no LLM" result and returns the worker error.
// The error is returned (not swallowed) so the pool records the failure while
// the user still gets visible SSE feedback instead of an endless spinner.
func (h *Handler) sendUnconfigured(ctx context.Context, hub *queue.SSEHub, job queue.Job, _ *llm.Client) error {
	errResult := map[string]any{
		signalSuggestions: []string{},
		signalSuggestErr:  "AI suggest failed: LLM not configured",
		signalAiPending:   false,
	}
	body, marshalErr := json.Marshal(queue.Job{Type: jobTypeSuggestResult, Payload: mustJSON(ctx, errResult)})
	if marshalErr == nil {
		if job.ClientID != "" {
			hub.Send(job.ClientID, body)
		} else {
			//nolint:contextcheck // SSEHub.Broadcast takes no context;
			// worker cancellation flows through the surrounding ctx.
			hub.Broadcast(body)
		}
	}
	return fmt.Errorf("llm client not configured for job %s", job.Type)
}

// mustJSON marshals for job envelopes; unmarshalable input cannot happen
// from the shapes above, so failure is logged, never returned.
func mustJSON(ctx context.Context, v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		obsArgs := append(observability.WithComponent("todo", "feature"), "error", err)
		observability.Error(ctx, "todo: marshal envelope", obsArgs...)
		return []byte("{}")
	}
	return b
}
