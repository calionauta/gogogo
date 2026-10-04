// SCOPE:layer=feature,removal=feature — Todo MVC example (reference implementation)
//
// The "retry_demo" job cluster: the UI trigger, the worker-side handler and
// the SSE feedback broadcast. Split out of todo.go so the queue/retry
// demonstration can be read on its own.
package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/pocketbase/pocketbase/core"
	sdk "github.com/starfederation/datastar-go/datastar"

	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	"github.com/calionauta/gogogo/internal/queue"
)

// handleEnqueueRetryDemo enqueues a "retry_demo" background job so the
// worker pool can exercise the queue + retry layer end-to-end. The job
// deliberately fails twice then succeeds, streaming per-attempt feedback
// to every connected client (see handleRetryDemoJob). Triggered from the
// Techstack/Diagnostics panel in the UI.
func (h *TodoHandler) handleEnqueueRetryDemo(c *core.RequestEvent) error {
	if err := h.q.Enqueue(context.Background(), mustJSON(queue.Job{Type: "retry_demo"})); err != nil {
		return c.String(statusInternal, "enqueue failed")
	}
	sse := sdk.NewSSE(c.Response, c.Request)
	return dshelpers.MergeSignals(sse, map[string]any{
		"lastRetry": "queued retry-demo job",
	})
}

// handleRetryDemoJob is the worker-side handler for "retry_demo" jobs. It
// runs a 3-attempt operation that fails on the first two attempts to make
// the retry layer (exponential backoff + SSE feedback) visible: each
// attempt's status is broadcast to every connected client, and a final
// toast reports success. This is the canonical demonstration of the
// queue-with-retry techstack slice.
// retryDemoInitialDelay spaces the retry attempts so the user can SEE
// the demo progress (the steps light one-by-one via the SSE "retry"
// feedback). A sub-second gap made all three attempts look instant; ~1.5s
// gives a perceptible beat between attempts without feeling sluggish.
const retryDemoInitialDelay = 1500 * time.Millisecond

// jobTypeToast is the queue.Job type for toast notifications so
// the literal isn't duplicated across handlers (goconst).
const jobTypeToast = "toast"

// jobTypeSuggestResult is the queue.Job type for AI suggest results.
const jobTypeSuggestResult = "suggest_result"

// phaseError is the shared "error" phase string used by both the
// onboarding stepper and the todo SSE dispatcher for error toasts.
const phaseError = "error"

func (h *TodoHandler) handleRetryDemoJob(ctx context.Context, hub *queue.SSEHub, _ queue.Job) error {
	const maxAttempts = 3
	attempt := 0
	err := retry.Do(
		func() error {
			attempt++
			// Deliberately fail the first two attempts to demonstrate
			// the retry layer; succeed on the final attempt.
			var opErr error
			if attempt < maxAttempts {
				opErr = fmt.Errorf("simulated transient failure on attempt %d", attempt)
			}
			h.broadcastRetryFeedback(hub, attempt, opErr)
			return opErr
		},
		retry.Attempts(maxAttempts),
		retry.Delay(retryDemoInitialDelay),
		retry.MaxDelay(2500*time.Millisecond), //nolint:mnd // 2.5s retry cap: visible pacing
		retry.Context(ctx),
	)
	if err != nil {
		hub.Broadcast(toastJob("Queue + retry demo failed", phaseError))
		return err
	}
	hub.Broadcast(toastJob("Queue + retry OK — 3 attempts", "success"))
	return nil
}

func (h *TodoHandler) broadcastRetryFeedback(hub *queue.SSEHub, attempt int, opErr error) {
	status := "attempt"
	if opErr == nil {
		status = "success"
	}
	payload := mustJSON(map[string]any{
		"operation": "retry-demo",
		"attempt":   attempt,
		"status":    status,
		"error":     errMsg(opErr),
	})
	hub.Broadcast(mustJSON(queue.Job{Type: "retry", Payload: payload}))
}
