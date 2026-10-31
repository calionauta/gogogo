// SCOPE:core — ephemeral cross-instance bridge contract tests.
package queue_test

import (
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/nats"
	"github.com/calionauta/gogogo/internal/queue"
)

// bridgedHubs boots an embedded NATS and two hubs on separate connections —
// two instances of the same app — with the EPHEMERAL bridge attached.
func bridgedHubs(t *testing.T) (*queue.SSEHub, *queue.SSEHub, *nats.Handle) {
	t.Helper()
	return twoHubsOn(t, (*queue.SSEHub).AttachEphemeral)
}

// receive asserts one delivery on ch within the deadline.
func receive(t *testing.T, ch <-chan []byte, what string) []byte {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(3 * time.Second):
		t.Fatalf("%s: no delivery", what)
		return nil
	}
}

// receiveNone asserts no delivery within a short grace window.
func receiveNone(t *testing.T, ch <-chan []byte, what string) {
	t.Helper()
	select {
	case got := <-ch:
		t.Fatalf("%s: unexpected delivery %s", what, got)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestEphemeralSendCrossesInstances is the chat case: the worker runs on
// instance A but the tab is connected to instance B, so the envelope must
// reach B's hub and stop there.
//
// RED-PROOFED: make AttachEphemeral a no-op (or publishEphemeral return
// false) and B never receives.
func TestEphemeralSendCrossesInstances(t *testing.T) {
	hubA, hubB, _ := bridgedHubs(t)
	chB := make(chan []byte, 4)
	hubB.Register("tab-b", "u1", chB)
	defer hubB.Unregister("tab-b")

	payload := []byte(`{"type":"chat","payload":{"role":"assistant"}}`)
	hubA.Send("tab-b", payload)

	if got := string(receive(t, chB, "B tab")); got != string(payload) {
		t.Fatalf("B received %q, want %q", got, payload)
	}
	receiveNone(t, chB, "B tab after first delivery (exactly once)")
}

// TestEphemeralBroadcastReachesEveryInstance is the onboarding/toast case:
// every instance's tabs see the signal exactly once, including the
// publisher's own (the bridge is the single delivery path, not a duplicate).
func TestEphemeralBroadcastReachesEveryInstance(t *testing.T) {
	hubA, hubB, _ := bridgedHubs(t)
	chA := make(chan []byte, 4)
	chB := make(chan []byte, 4)
	hubA.Register("tab-a", "u1", chA)
	hubB.Register("tab-b", "u1", chB)
	defer hubA.Unregister("tab-a")
	defer hubB.Unregister("tab-b")

	payload := []byte(`{"type":"workflow-progress","payload":{"step":2}}`)
	hubA.Broadcast(payload)

	receive(t, chA, "A tab")
	receive(t, chB, "B tab")
	receiveNone(t, chA, "A tab after first delivery (exactly once)")
	receiveNone(t, chB, "B tab after first delivery (exactly once)")
}

// TestEphemeralBridgeLeavesRecordAndDocFanoutAlone pins the boundary:
// BroadcastToUser/BroadcastToAllUsers (record events, already carried by
// APP_CRUD) and the doc-scoped methods (app.sync) must NOT ride the ephemeral
// subject, or every record change would be delivered twice cross-instance.
func TestEphemeralBridgeLeavesRecordAndDocFanoutAlone(t *testing.T) {
	hubA, hubB, _ := bridgedHubs(t)
	chB := make(chan []byte, 4)
	hubB.Register("tab-b", "u1", chB)
	defer hubB.Unregister("tab-b")

	payload := []byte(`{"event":"created","id":"t1"}`)
	hubA.BroadcastToUser(payload, "u1", "")
	hubA.BroadcastToAllUsers(payload, "")
	hubA.BroadcastToDoc(payload, "doc-1")
	hubA.BroadcastExceptToDoc(payload, "doc-1", "someone")

	receiveNone(t, chB, "record/doc fan-out on the ephemeral subject")
}

// TestEphemeralFallsBackLocallyWhenPublishFails: the bus being down must not
// cost the tabs on this instance their signal.
func TestEphemeralFallsBackLocallyWhenPublishFails(t *testing.T) {
	hubA, _, connA := bridgedHubs(t)
	chA := make(chan []byte, 4)
	hubA.Register("tab-a", "u1", chA)
	defer hubA.Unregister("tab-a")

	connA.Close() // subsequent Publish fails

	payload := []byte(`{"type":"toast","payload":{"message":"hi"}}`)
	hubA.Send("tab-a", payload)
	if got := string(receive(t, chA, "local tab after publish failure")); got != string(payload) {
		t.Fatalf("local fallback received %q", got)
	}
}

// TestEphemeralForeignTargetIsNotBuffered: an instance that does not hold the
// tab must DROP a bridged targeted signal, never buffer it. Buffering would
// replay a stale chat answer when that clientID later connects here.
//
// RED-PROOFED: route the bridge through Send (buffering path) and the late
// registration receives the old payload.
func TestEphemeralForeignTargetIsNotBuffered(t *testing.T) {
	hubA, hubB, _ := bridgedHubs(t)

	payload := []byte(`{"type":"chat","payload":{"role":"assistant","text":"old"}}`)
	hubA.Send("tab-late", payload)
	time.Sleep(200 * time.Millisecond) // let the bridge deliver/drop

	chLate := make(chan []byte, 4)
	hubB.Register("tab-late", "u1", chLate)
	defer hubB.Unregister("tab-late")
	receiveNone(t, chLate, "late tab (stale buffered bridged signal)")
}
