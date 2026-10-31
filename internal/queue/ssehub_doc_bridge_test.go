// SCOPE:core — doc-scoped cross-instance bridge contract tests.
package queue_test

import (
	"testing"

	"github.com/calionauta/gogogo/internal/nats"
	"github.com/calionauta/gogogo/internal/queue"
)

// docBridgedHubs boots two instances serving tabs on the same board, with the
// DOC bridge attached on both.
func docBridgedHubs(t *testing.T) (*queue.SSEHub, *queue.SSEHub, *nats.Handle) {
	t.Helper()
	return twoHubsOn(t, (*queue.SSEHub).AttachDocBridge)
}

// TestDocBridgeBroadcastToDocCrossesInstances is the presence-count case: the
// roster recount is produced on the instance that saw the join, and the tab
// watching the same board is connected to the other one.
//
// RED-PROOFED: drop the publishDocBridge branch from BroadcastToDoc (or make
// AttachDocBridge a no-op) and B never receives the count.
func TestDocBridgeBroadcastToDocCrossesInstances(t *testing.T) {
	hubA, hubB, _ := docBridgedHubs(t)
	chB := make(chan []byte, 4)
	hubB.Register("tab-b", "u1", chB)
	hubB.SetDoc("tab-b", "doc-1")
	defer hubB.Unregister("tab-b")

	payload := []byte(`{"type":"count","doc":"doc-1","peers":["tab-a","tab-b"]}`)
	hubA.BroadcastToDoc(payload, "doc-1")

	if got := string(receive(t, chB, "B tab count")); got != string(payload) {
		t.Fatalf("B received %q, want %q", got, payload)
	}
	receiveNone(t, chB, "B tab after first delivery (exactly once)")
}

// TestDocBridgeExceptToDocCrossesInstances is the join/leave/cursor case: the
// originator must not hear its own echo, but a peer on another instance must.
//
// RED-PROOFED: pass the empty except through the wire (drop the Except field
// from the envelope) and the originator's own tab receives the echo.
func TestDocBridgeExceptToDocCrossesInstances(t *testing.T) {
	hubA, hubB, _ := docBridgedHubs(t)
	chA := make(chan []byte, 4)
	chB := make(chan []byte, 4)
	hubA.Register("tab-a", "u1", chA)
	hubA.SetDoc("tab-a", "doc-1")
	defer hubA.Unregister("tab-a")
	hubB.Register("tab-b", "u2", chB)
	hubB.SetDoc("tab-b", "doc-1")
	defer hubB.Unregister("tab-b")

	payload := []byte(`{"type":"join","doc":"doc-1","user":"tab-a"}`)
	hubA.BroadcastExceptToDoc(payload, "doc-1", "tab-a")

	// The peer on the OTHER instance sees it...
	if got := string(receive(t, chB, "B peer join")); got != string(payload) {
		t.Fatalf("B received %q, want %q", got, payload)
	}
	// ...and the originator does not, on either instance.
	receiveNone(t, chA, "A originator echo")
	receiveNone(t, chB, "B tab after first delivery (exactly once)")
}

// TestDocBridgeStaysOnItsOwnDoc pins the doc scope on the wire: a count for
// doc-1 must never reach a tab watching doc-2, even when both instances hold
// both docs.
//
// RED-PROOFED: have deliverDocLocal ignore h.docOf (a global broadcast) and
// the doc-2 tab receives doc-1's roster.
func TestDocBridgeStaysOnItsOwnDoc(t *testing.T) {
	hubA, hubB, _ := docBridgedHubs(t)
	sameDoc := make(chan []byte, 4)
	otherDoc := make(chan []byte, 4)
	hubB.Register("tab-same", "u1", sameDoc)
	hubB.SetDoc("tab-same", "doc-1")
	defer hubB.Unregister("tab-same")
	hubB.Register("tab-other", "u1", otherDoc)
	hubB.SetDoc("tab-other", "doc-2")
	defer hubB.Unregister("tab-other")

	payload := []byte(`{"type":"count","doc":"doc-1","peers":["tab-x"]}`)
	hubA.BroadcastToDoc(payload, "doc-1")

	receive(t, sameDoc, "B tab on doc-1")
	receiveNone(t, otherDoc, "B tab on doc-2 (cross-doc leak)")
}

// TestDocBridgeDoesNotRideEphemeral pins the boundary the other way: an
// ephemeral Broadcast must not be re-delivered by the doc bridge, and a doc
// event must not ride app.ephemeral — two wires, one delivery each.
//
// RED-PROOFED: point both bridges at the same subject and every assertion
// below sees a duplicate delivery.
func TestDocBridgeDoesNotRideEphemeral(t *testing.T) {
	hubA, hubB, _ := docBridgedHubs(t)
	if err := hubA.AttachEphemeral(nil); err != nil { // nil = stays local, no wire
		t.Fatalf("attach ephemeral nil: %v", err)
	}
	chB := make(chan []byte, 4)
	hubB.Register("tab-b", "u1", chB)
	hubB.SetDoc("tab-b", "doc-1")
	defer hubB.Unregister("tab-b")

	// A doc event crosses exactly once (the doc bridge).
	docPayload := []byte(`{"type":"leave","doc":"doc-1","from":"tab-a"}`)
	hubA.BroadcastExceptToDoc(docPayload, "doc-1", "tab-a")
	receive(t, chB, "B doc event")
	receiveNone(t, chB, "B doc event duplicate (ephemeral cross-talk)")

	// A broadcast on the publisher's own instance reaches it once.
	chA := make(chan []byte, 4)
	hubA.Register("tab-a", "u1", chA)
	hubA.SetDoc("tab-a", "doc-1")
	defer hubA.Unregister("tab-a")
	hubA.Broadcast([]byte(`{"type":"toast"}`))
	receive(t, chA, "A local broadcast")
	receiveNone(t, chA, "A local broadcast duplicate")
}

// TestDocBridgeFallsBackLocallyWhenPublishFails: the bus being down must not
// cost the tabs on this instance the event.
func TestDocBridgeFallsBackLocallyWhenPublishFails(t *testing.T) {
	hubA, _, connA := docBridgedHubs(t)
	chA := make(chan []byte, 4)
	hubA.Register("tab-a", "u1", chA)
	hubA.SetDoc("tab-a", "doc-1")
	defer hubA.Unregister("tab-a")

	connA.Close() // subsequent Publish fails

	payload := []byte(`{"type":"count","doc":"doc-1","peers":["tab-a"]}`)
	hubA.BroadcastToDoc(payload, "doc-1")
	if got := string(receive(t, chA, "local tab after publish failure")); got != string(payload) {
		t.Fatalf("local fallback received %q", got)
	}
}
