// SCOPE:layer=infra,removal=plugin — gogen-ui AG-UI envelope tests.
package genui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/queue"
)

// TestEncodeTextMessageContentWireFormat pins the AG-UI wire contract our
// emitter must speak: SCREAMING type + camelCase fields, byte-compatible
// with what the reference Go SDK (ag-ui-protocol/ag-ui, sdks/community/go)
// produces for the same event. If upstream renames a field, this goes red.
func TestEncodeTextMessageContentWireFormat(t *testing.T) {
	t.Parallel()
	raw, err := Encode(Event{Type: EventTypeTextMessageContent, MessageID: "m1", Delta: "hi"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode own output: %v", err)
	}
	if got["type"] != "TEXT_MESSAGE_CONTENT" || got["messageId"] != "m1" || got["delta"] != "hi" {
		t.Fatalf("wire = %s, want type/messageId/delta", raw)
	}
}

// TestDecodeReadsReferenceSDKOutput proves interop without the dependency:
// bytes shaped exactly like the reference SDK's TextMessageContentEvent
// must parse into our Event with fields intact.
func TestDecodeReadsReferenceSDKOutput(t *testing.T) {
	t.Parallel()
	raw := `{"type":"TEXT_MESSAGE_CONTENT","messageId":"m1","delta":"hi","timestamp":123}`
	evt, err := Decode([]byte(raw))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if evt.Type != EventTypeTextMessageContent || evt.MessageID != "m1" || evt.Delta != "hi" {
		t.Fatalf("decoded = %+v, want content m1/hi", evt)
	}
}

// TestDecodeRejectsUnknownType is the fail-closed contract: an unknown
// type must error, never produce a zero Event that a renderer would
// silently drop (or worse, render as the wrong component).
func TestDecodeRejectsUnknownType(t *testing.T) {
	t.Parallel()
	if _, err := Decode([]byte(`{"type":"NOPE"}`)); err == nil {
		t.Fatal("Decode(unknown type) = nil error, want rejection")
	}
	if _, err := Decode([]byte(`not json`)); err == nil {
		t.Fatal("Decode(garbage) = nil error, want rejection")
	}
}

// TestEncodeRejectsUnregisteredType proves Encode validates too: callers
// cannot emit a type outside the registry even when skipping Decode.
func TestEncodeRejectsUnregisteredType(t *testing.T) {
	t.Parallel()
	if _, err := Encode(Event{Type: "NOPE", MessageID: "m1"}); err == nil {
		t.Fatal("Encode(unknown type) = nil error, want rejection")
	}
}

// TestEmitDeliversThroughHub proves transport compatibility: an encoded
// AG-UI event travels the existing SSE Hub to a registered client and
// decodes back to the same event. No new transport, no new dependency.
func TestEmitDeliversThroughHub(t *testing.T) {
	t.Parallel()
	hub := queue.NewSSEHub()
	ch := make(chan []byte, 4)
	hub.Register("c1", "u1", ch)
	defer hub.Unregister("c1")

	want := Event{Type: EventTypeTextMessageContent, MessageID: "m1", Delta: "hi"}
	if err := Emit(hub, "c1", want); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	select {
	case raw := <-ch:
		got, err := Decode(raw)
		if err != nil {
			t.Fatalf("Decode(hub bytes): %v", err)
		}
		if got != want {
			t.Fatalf("round-trip = %+v, want %+v", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no bytes arrived on hub channel")
	}
}

// TestRunLifecycleEnvelope pins the run open/close pair an external
// AG-UI client needs to frame a stream (mirrors RUN_STARTED/RUN_FINISHED
// in the reference SDK).
func TestRunLifecycleEnvelope(t *testing.T) {
	t.Parallel()
	started, err := Encode(Event{Type: EventTypeRunStarted, RunID: "r1", ThreadID: "t1"})
	if err != nil {
		t.Fatalf("Encode started: %v", err)
	}
	if !strings.Contains(string(started), `"type":"RUN_STARTED"`) {
		t.Fatalf("started = %s, want RUN_STARTED", started)
	}
	finished, err := Encode(Event{Type: EventTypeRunFinished, RunID: "r1", ThreadID: "t1"})
	if err != nil {
		t.Fatalf("Encode finished: %v", err)
	}
	evt, err := Decode(finished)
	if err != nil {
		t.Fatalf("Decode finished: %v", err)
	}
	if evt.Type != EventTypeRunFinished || evt.RunID != "r1" {
		t.Fatalf("decoded = %+v, want run finished r1", evt)
	}
}
