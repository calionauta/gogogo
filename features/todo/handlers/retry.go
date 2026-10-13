// SCOPE:feature — HTTP todo retry-demo slice (queue + SSE feedback)
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/avast/retry-go/v4"
	sdk "github.com/starfederation/datastar-go/datastar"

	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	"github.com/calionauta/gogogo/internal/queue"
)

// retryDemoInitialDelay spaces attempts so a human sees the stepper move.
const retryDemoInitialDelay = 1500 * time.Millisecond

// retryDemoMaxDelay caps the backoff so the demo stays perceptible.
const retryDemoMaxDelay = 2500 * time.Millisecond

// handleRetryDemo enqueues a retry-demo job: the worker fails twice then
// succeeds, streaming per-attempt feedback over SSE.
func (h *Handler) handleRetryDemo(w http.ResponseWriter, r *http.Request) {
	if h.Queue == nil {
		h.fail(r.Context(), w, "todo: retry-demo queue missing", errNoQueue, "enqueue failed")
		return
	}
	// Thread the originating tab through the job: the worker routes
	// per-attempt feedback back to it. Dropping the clientID left the
	// feedback unroutable and the stepper stuck on step 1 forever.
	body, err := json.Marshal(queue.Job{Type: signalJobRetryDemo, ClientID: r.URL.Query().Get("clientID")})
	if err != nil {
		h.fail(r.Context(), w, "todo: retry-demo marshal failed", err, "enqueue failed")
		return
	}
	if err := h.Queue.Enqueue(r.Context(), body); err != nil {
		h.fail(r.Context(), w, "todo: retry-demo enqueue failed", err, "enqueue failed")
		return
	}
	sse := sdk.NewSSE(w, r)
	if err := dshelpers.MergeSignals(sse, map[string]any{
		"lastRetry": "queued retry-demo job",
	}); err != nil {
		h.fail(r.Context(), w, "todo: retry-demo ack failed", err, "enqueue failed")
	}
}

// RegisterRetryHandlers wires the retry-demo worker. Call with the same
// registry the routes enqueue to, before StartWorkers.
func (h *Handler) RegisterRetryHandlers(reg *queue.HandlerRegistry) {
	reg.Register(signalJobRetryDemo, h.handleRetryDemoJob)
}

func (h *Handler) handleRetryDemoJob(ctx context.Context, hub *queue.SSEHub, job queue.Job) error {
	const maxAttempts = 3
	attempt := 0
	err := retry.Do(
		func() error {
			attempt++
			var opErr error
			if attempt < maxAttempts {
				opErr = fmt.Errorf("simulated transient failure on attempt %d", attempt)
			}
			broadcastRetryFeedback(ctx, hub, job.ClientID, attempt, opErr)
			return opErr
		},
		retry.Attempts(maxAttempts),
		retry.Delay(retryDemoInitialDelay),
		retry.MaxDelay(retryDemoMaxDelay),
		retry.Context(ctx),
	)
	if err != nil {
		sendToOriginator(ctx, hub, job.ClientID, toastJob("Queue + retry demo failed", "error"))
		return err
	}
	sendToOriginator(ctx, hub, job.ClientID, toastJob("Queue + retry OK — 3 attempts", "success"))
	return nil
}

// sendToOriginator delivers a worker envelope to the tab that started
// the job, falling back to a broadcast when the job carries no client
// (a global demo every tab watches). Targeted delivery keeps one user's
// demo progress off every other user's stream.
func sendToOriginator(_ context.Context, hub *queue.SSEHub, clientID string, body []byte) {
	if clientID == "" {
		//nolint:contextcheck // SSEHub.Broadcast takes no context; worker
		// cancellation flows through the surrounding ctx select.
		hub.Broadcast(body)
		return
	}
	hub.Send(clientID, body)
}

func broadcastRetryFeedback(ctx context.Context, hub *queue.SSEHub, clientID string, attempt int, opErr error) {
	status := "attempt"
	if opErr == nil {
		status = "success"
	}
	msg := ""
	if opErr != nil {
		msg = opErr.Error()
	}
	payload, _ := json.Marshal(map[string]any{
		"operation": signalJobRetryDemo,
		"attempt":   attempt,
		"status":    status,
		"error":     msg,
	})
	sendToOriginator(ctx, hub, clientID, mustJSON(ctx, queue.Job{Type: "retry", Payload: payload}))
}

func toastJob(message, kind string) []byte {
	payload, _ := json.Marshal(map[string]string{"message": message, "toastType": kind})
	body, _ := json.Marshal(queue.Job{Type: jobTypeToast, Payload: payload})
	return body
}
