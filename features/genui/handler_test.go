// SCOPE:layer=feature,removal=feature — gogen-ui worker tests.
package genui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/internal/queue"
)

// stubResponder is the function-field injection for the model leg:
// production passes an llm adapter, tests pass canned JSON.
type stubResponder struct {
	raw string
	err error
}

func (s stubResponder) Respond(_ context.Context, _ string) (string, error) {
	return s.raw, s.err
}

// TestWorkerDeliversComponents proves the full async path without a model:
// prompt in, genui envelope with rendered component HTML out on the
// originator's hub channel. Uses real SSEHubs (no mocks of infra) —
// and crucially TWO of them: the pool passes its shared hub as the
// argument, but delivery must land on the feature's own hub. A single
// hub in this test would hide the cross-talk bug that once surfaced
// Ask answers as toasts on the Todo tab.
func TestWorkerDeliversComponents(t *testing.T) {
	t.Parallel()
	ownHub := queue.NewSSEHub()
	ch := make(chan []byte, 8)
	ownHub.Register("c1", "u1", ch)
	defer ownHub.Unregister("c1")
	poolHub := queue.NewSSEHub()
	poolCh := make(chan []byte, 8)
	poolHub.Register("c1", "u1", poolCh)
	defer poolHub.Unregister("c1")

	h := &Handler{hub: ownHub, responder: stubResponder{raw: `{"components":[` +
		`{"type":"text_note","props":{"text":"hello"}},` +
		`{"type":"plan_cards","props":{"plans":[{"title":"Ship it","detail":"today"}]}}]}`}}
	payload, _ := json.Marshal(map[string]string{"prompt": "plan my day", "userId": "u1"})
	job := queue.Job{Type: "genui_ask", ClientID: "c1", Payload: payload}

	if err := h.handleGenuiJob(context.Background(), poolHub, job); err != nil {
		t.Fatalf("handleGenuiJob: %v", err)
	}
	foundHTML, foundToast := false, false
	timeout := time.After(2 * time.Second)
	for !foundHTML || !foundToast {
		select {
		case raw := <-ch:
			s := string(raw)
			if strings.Contains(s, "hello") && strings.Contains(s, "Ship it") {
				foundHTML = true
			}
			if strings.Contains(s, `"type":"toast"`) || strings.Contains(s, "toast") {
				foundToast = true
			}
		case <-timeout:
			t.Fatalf("hub got html=%v toast=%v, want both", foundHTML, foundToast)
		}
	}
	select {
	case raw := <-poolCh:
		t.Fatalf("pool hub received genui traffic (cross-talk): %s", raw)
	default:
	}
}

// TestWorkerRejectsUnknownDirective proves the fail-closed path: a model
// answering outside the catalog surfaces an error toast, and NO component
// HTML reaches the tab. The worker returns nil (no pool retry — the llm
// client already retried transient faults, and a retry would re-run
// minutes of free-model latency while broadcasting noise to a foreign
// hub), so the error RESULT is the contract, not the return.
func TestWorkerRejectsUnknownDirective(t *testing.T) {
	t.Parallel()
	ownHub := queue.NewSSEHub()
	ch := make(chan []byte, 8)
	ownHub.Register("c1", "u1", ch)
	defer ownHub.Unregister("c1")
	poolHub := queue.NewSSEHub()

	h := &Handler{hub: ownHub, responder: stubResponder{raw: `{"components":[{"type":"evil","props":{"text":"x"}}]}`}}
	payload, _ := json.Marshal(map[string]string{"prompt": "x", "userId": "u1"})
	job := queue.Job{Type: "genui_ask", ClientID: "c1", Payload: payload}

	if err := h.handleGenuiJob(context.Background(), poolHub, job); err != nil {
		t.Fatalf("handleGenuiJob(unknown) = %v, want nil (no pool retry; error delivered as result)", err)
	}
	select {
	case raw := <-ch:
		if strings.Contains(string(raw), "evil") {
			t.Fatalf("unknown directive leaked component HTML: %s", raw)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no error toast arrived on hub channel")
	}
}

// TestWorkerReleasesSpinnerWhenModelDown proves the unconfigured path:
// no responder output still releases the Ask button spinner. The worker
// delivers an error envelope; the dispatcher turns it into
// genui_pending=false on the stream — this test drives both layers with
// a real hub and a recorded SSE response (same shape as the todo
// spinner regression test).
func TestWorkerReleasesSpinnerWhenModelDown(t *testing.T) {
	t.Parallel()
	hub := queue.NewSSEHub()
	ch := make(chan []byte, 8)
	hub.Register("c1", "u1", ch)
	defer hub.Unregister("c1")

	h := &Handler{hub: hub, responder: stubResponder{err: errors.New("boom")}}
	payload, _ := json.Marshal(map[string]string{"prompt": "x", "userId": "u1"})
	job := queue.Job{Type: "genui_ask", ClientID: "c1", Payload: payload}

	_ = h.handleGenuiJob(context.Background(), hub, job)
	var envelope []byte
	select {
	case envelope = <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("no error result arrived on hub channel")
	}
	rec := httptest.NewRecorder()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "/api/genui/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	sse := sdk.NewSSE(rec, req)
	if err := h.dispatchStreamMessage(sse, envelope); err != nil {
		t.Fatalf("dispatch error envelope: %v", err)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"genui_pending":false`) {
		t.Fatalf("dispatch must release the spinner signal, got:\n%s", body)
	}
}
