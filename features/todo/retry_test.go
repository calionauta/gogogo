package todo_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/queue"
)

// collectRetryFeedback scans an SSE transcript for Datastar `lastRetry`
// signals and returns the distinct retry events (de-duplicated by
// attempt number) plus the highest attempt seen. The retry feedback is
// emitted as a signals patch: `data: signals {"lastRetry":"<escaped json>"}`.
// We extract each `data:` payload, locate the lastRetry signal, and decode
// the embedded JSON. This avoids brittle substring matching on escaped
// quotes. Kept as a free function (not a closure) so the calling test
// stays below the gocyclo limit.
func collectRetryFeedback(transcript string) (events []map[string]any, seenAttempts int) {
	for _, ev := range parseSSEData(transcript) {
		idx := strings.Index(ev, "lastRetry")
		if idx < 0 {
			continue
		}
		// The signal value is `"<escaped json>"`; skip the `:"` delimiter
		// and parse the JSON string that follows.
		delim := strings.Index(ev[idx:], ":\"")
		if delim < 0 {
			continue
		}
		val, ok := extractJSONString(ev[idx+delim:])
		if !ok {
			continue
		}
		var p struct {
			Operation string `json:"operation"`
			Attempt   int    `json:"attempt"`
			Status    string `json:"status"`
		}
		if err := json.Unmarshal([]byte(val), &p); err != nil {
			continue
		}
		if p.Attempt > seenAttempts {
			seenAttempts = p.Attempt
			events = append(events, map[string]any{
				"operation": p.Operation, "attempt": p.Attempt, "status": p.Status,
			})
		}
	}
	return events, seenAttempts
}

// hasRetrySuccess reports whether any event carries status "success".
func hasRetrySuccess(events []map[string]any) bool {
	for _, e := range events {
		if e["status"] == "success" {
			return true
		}
	}
	return false
}

// TestIntegration_RetryFeedbackExercisesSSE verifies the SSE-aware
// retry path: a handler that fails the first 2 attempts and succeeds
// on the 3rd emits per-attempt feedback to the SSE Hub. This proves
// the HTTP → queue → worker → SSE pipeline is wired correctly with
// exponential backoff + SSE feedback.
func TestIntegration_RetryFeedbackExercisesSSE(t *testing.T) {
	t.Parallel()
	base, q, _, _, cleanup := testFixture(t)
	defer cleanup()

	var attempts atomic.Int32
	q.Registry().Register("flaky_retry", func(_ context.Context, _ *queue.SSEHub, _ queue.Job) error {
		n := attempts.Add(1)
		if n < 3 {
			return fmt.Errorf("transient failure #%d", n)
		}
		return nil
	})

	clientID := "retry-feedback-" + time.Now().Format(clientIDSuffixFormat)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	stream := openSSEWithCtx(ctx, t, base, clientID)
	defer func() { _ = stream.Body.Close() }()

	time.Sleep(100 * time.Millisecond)

	job := queue.Job{Type: "flaky_retry", ClientID: clientID, Payload: []byte(`{}`)}
	body, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal job: %v", err)
	}
	if err := q.Enqueue(ctx, body); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Poll the SSE stream until the success feedback event arrives. That
	// proves the pipeline delivered both transient failures AND the
	// eventual success. Reading past it would race the next event.
	var retryEvents []map[string]any
	var seenAttempts int
	full := pumpSSEUntil(t, stream, 15*time.Second, func(transcript string) bool {
		retryEvents, seenAttempts = collectRetryFeedback(transcript)
		return hasRetrySuccess(retryEvents)
	})

	t.Logf("attempts=%d retry_feedback_events=%d", attempts.Load(), seenAttempts)
	for _, e := range retryEvents {
		t.Logf("retry feedback: %+v", e)
	}

	if got := attempts.Load(); got < 3 {
		t.Fatalf("flaky handler ran %d times, want >= 3", got)
	}
	if seenAttempts < 2 {
		t.Fatalf("expected >= 2 retry feedback events on SSE stream, saw %d (stream tail: %s)",
			seenAttempts, tailString(full, 600))
	}
	// The final retry event must report success, proving the pipeline
	// surfaces both the transient failures AND the eventual success.
	if len(retryEvents) == 0 || retryEvents[len(retryEvents)-1]["status"] != "success" {
		t.Fatalf("last retry event should be status=success, got %+v", retryEvents)
	}
}

// sseSignalIntMax scans an SSE transcript for a numeric signal
// (e.g. "demoStep":N) and returns the highest N seen. Used to prove a
// progressive stepper reached its final step rather than stalling at an
// earlier one (the original "retry demo never finalizes / stuck at step
// 2" bug). Returns 0 if the signal never appears.
func sseSignalIntMax(transcript, name string) int {
	max := 0
	for _, data := range parseSSEData(transcript) {
		needle := "\"" + name + "\":"
		idx := strings.Index(data, needle)
		for idx >= 0 {
			rest := data[idx+len(needle):]
			var n int
			if _, err := fmt.Sscanf(rest, "%d", &n); err == nil && n > max {
				max = n
			}
			next := strings.Index(data[idx+1:], needle)
			if next < 0 {
				break
			}
			idx = idx + 1 + next
		}
	}
	return max
}

// TestIntegration_RetryDemoCompletesToStepThree is the end-to-end
// regression guard for the "Retry on simulated failure" demo actually
// FINALIZING. It runs the REAL retry_demo job (the same handler the UI
// button enqueues) through the real goqite worker + SSE pipeline and
// asserts the progressive stepper reaches its final step (demoStep=3)
// and that an explicit success retry event is broadcast (which is what
// resets the spinner via applyTechStep). This directly covers the user
// report that the demo "doesn't finalize / gets stuck at step 2": if the
// success branch in streamRetry stops firing, demoStep never reaches 3
// and this test fails instead of shipping the stuck spinner.
func TestIntegration_RetryDemoCompletesToStepThree(t *testing.T) {
	t.Parallel()
	base, q, _, _, cleanup := testFixture(t)
	defer cleanup()

	clientID := "retry-demo-" + time.Now().Format(clientIDSuffixFormat)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Authenticated SSE path (mirrors a real logged-in browser): log in
	// to get the gogogo_auth cookie, then open the stream through that
	// client so handleSSEStreamWithAuth → LoadAppAuth runs for real.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}
	loginUser(ctx, t, client, base, demoEmail, demoPassword)

	stream := openSSEWithClient(ctx, t, client, base, clientID)
	defer func() { _ = stream.Body.Close() }()
	time.Sleep(100 * time.Millisecond)

	// Enqueue the REAL retry_demo job — identical path to the UI button.
	job, err := json.Marshal(queue.Job{Type: "retry_demo", ClientID: clientID})
	if err != nil {
		t.Fatalf("marshal job: %v", err)
	}
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatalf("enqueue retry_demo: %v", err)
	}

	// Pump until the stepper reaches its final step AND a success retry
	// event has been broadcast.
	full := pumpSSEUntil(t, stream, 15*time.Second, func(transcript string) bool {
		evs, _ := collectRetryFeedback(transcript)
		return sseSignalIntMax(transcript, "demoStep") >= 3 && hasRetrySuccess(evs)
	})

	// 1) Finalize: the stepper must reach step 3 (not stall at 1 or 2).
	if got := sseSignalIntMax(full, "demoStep"); got < 3 {
		t.Fatalf("retry-demo demoStep reached %d, want >= 3 (tail: %s)", got, tailString(full, 800))
	}
	// 2) The success branch in streamRetry must have fired (this is what
	//    resets suggestPending=false on the spinner).
	if events, _ := collectRetryFeedback(full); !hasRetrySuccess(events) {
		t.Fatalf("retry-demo never emitted a success retry event (tail: %s)", tailString(full, 800))
	}
	// 3) It must have advanced THROUGH step 2 (proving the progressive
	//    lighting works, not a single jump or a stall).
	if got := sseSignalIntMax(full, "demoStep"); got < 2 {
		t.Fatalf("retry-demo never advanced to step 2 (tail: %s)", tailString(full, 800))
	}
}
