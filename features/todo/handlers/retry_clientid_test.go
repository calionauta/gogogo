// SCOPE:feature — Todo retry-demo clientID propagation red-proof.
//
// The Queue + Retry demo POSTs with ?clientID=<tab> so worker feedback
// (attempt/success envelopes + toasts) can be routed back to the tab
// that started the demo. handleRetryDemo must thread that clientID into
// the enqueued job; dropping it leaves the worker's feedback
// unroutable and the stepper stuck on step 1 forever.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/queue"
)

// TestHandlersRetryDemoPropagatesClientID pins that the enqueued
// retry-demo job carries the request's clientID for per-tab routing.
func TestHandlersRetryDemoPropagatesClientID(t *testing.T) {
	f := newSuggestFixture(t, true, false)

	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/todos/retry-demo?clientID=tab-1", nil)
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("retry-demo: status = %d (body: %s)", rec.Code, rec.Body.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	msg, err := f.h.Queue.ReceiveAndWait(ctx, 4*time.Second)
	if err != nil {
		t.Fatalf("receive enqueued job: %v", err)
	}
	var job queue.Job
	if err := json.Unmarshal(msg.Body, &job); err != nil {
		t.Fatalf("decode enqueued job: %v", err)
	}
	if job.Type != signalJobRetryDemo {
		t.Fatalf("enqueued job type = %q, want %q", job.Type, signalJobRetryDemo)
	}
	if job.ClientID != "tab-1" {
		t.Fatalf("enqueued job clientID = %q, want %q (feedback cannot route back)", job.ClientID, "tab-1")
	}
}
