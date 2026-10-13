// SCOPE:feature — retry-envelope routing contract tests.
//
// The queue's RetryConfig.Do wraps every handler, so ANY job type emits a
// retry envelope with operation = job.Type. Only the operations that own a
// UI stepper may drive one; a job that declares none must not borrow the
// retry demo's. These tests pin that in both directions.
package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/starfederation/datastar-go/datastar"
)

// sseGen builds an SSE generator over a recorder for the signal-merge
// tests, which have no live request to borrow a context from. It lives here
// rather than in onboarding_test.go because the dagnats trim keeps this
// file: a helper used by tests that survive a trim cannot live in one that
// does not.
func sseGen(tb testing.TB, w http.ResponseWriter) *sdk.ServerSentEventGenerator {
	tb.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/todos", nil)
	return sdk.NewSSE(w, req)
}

// retryEventFor builds the envelope the queue's retry layer emits for one
// attempt of one job type.
func retryEventFor(operation, status string) []byte {
	return []byte(`{"operation":"` + operation + `","attempt":1,"status":"` + status + `","error":""}`)
}

func TestRetryOnlyDrivesItsOwnStepper(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		status    string
		wantDemo  bool
		wantAI    bool
	}{
		{"suggest success lights the ai stepper", signalJobSuggest, retryStatusSuccess, false, true},
		{"simulated suggest lights the ai stepper", signalJobSuggestSimulated, retryStatusSuccess, false, true},
		{"retry demo lights the demo stepper", signalJobRetryDemo, retryStatusSuccess, true, false},
		// The regression: genui and chat deliver their own result + toast and
		// return nil unconditionally, so their success envelope used to be read
		// as the retry demo and mark all three of ITS steps done.
		{"genui drives nothing", "genui_ask", retryStatusSuccess, false, false},
		{"genui failure drives nothing", "genui_ask", "attempt", false, false},
		{"chat drives nothing", "chat_ask", retryStatusSuccess, false, false},
		{"chat failure drives nothing", "chat_ask", "attempt", false, false},
		{"unknown job drives nothing", "something_new", retryStatusSuccess, false, false},
	}
	h := &Handler{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			sse := sseGen(t, rec)
			if err := h.streamRetry(sse, retryEventFor(tc.operation, tc.status)); err != nil {
				t.Fatalf("streamRetry: %v", err)
			}
			out := rec.Body.String()
			if got := strings.Contains(out, `"demoStep"`); got != tc.wantDemo {
				t.Errorf("demoStep lit = %v, want %v (body: %s)", got, tc.wantDemo, out)
			}
			if got := strings.Contains(out, `"aiStep"`); got != tc.wantAI {
				t.Errorf("aiStep lit = %v, want %v (body: %s)", got, tc.wantAI, out)
			}
		})
	}
}

// TestRetryWithoutStepperEmitsNothing is the strict half: a job with no
// stepper produces no output at all — no signals, no toast. The contradictory
// "<op>: completed" toast beside the worker's own "Ask failed" toast is what
// made the bug visible in the first place, so both must be gone.
func TestRetryWithoutStepperEmitsNothing(t *testing.T) {
	h := &Handler{}
	for _, op := range []string{"genui_ask", "chat_ask", "something_new"} {
		t.Run(op, func(t *testing.T) {
			rec := httptest.NewRecorder()
			sse := sseGen(t, rec)
			if err := h.streamRetry(sse, retryEventFor(op, retryStatusSuccess)); err != nil {
				t.Fatalf("streamRetry: %v", err)
			}
			if out := rec.Body.String(); strings.TrimSpace(out) != "" {
				t.Fatalf("%s must emit nothing, got:\n%s", op, out)
			}
		})
	}
}
