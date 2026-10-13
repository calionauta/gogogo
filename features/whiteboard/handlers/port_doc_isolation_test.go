// SCOPE:feature — Whiteboard cross-doc isolation red-proof.
//
// Two tabs on DIFFERENT boards share one SSE hub. A shapes event for
// docA must never land on docB's stream: without doc scoping every tab
// adopts the last broadcast from ANY board, so each side's drawing
// overwrites the other's view (the reported "drawings erased" bug).
package handlers

import (
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/collab"
)

// TestWhiteboardCrossDocIsolation pins that a shape drawn on docA is
// invisible to a stream watching docB, while still reaching docA peers.
func TestWhiteboardCrossDocIsolation(t *testing.T) {
	server, _, cookie := wbPortFixture(t)
	client := wbClient(t, server, cookie)
	docA := "doc-isolation-a"
	docB := "doc-isolation-b"

	streamA := openWBStream(t, client, server, docA, "wbA")
	defer streamA.close()
	streamB := openWBStream(t, client, server, docB, "wbC")
	defer streamB.close()
	streamA.settleJoin()
	streamB.settleJoin()

	postOp(t, client, server, docA, "wbWriter",
		collab.ShapeOp{Op: "add", Shape: collab.Shape{ID: "only-a", Type: "rect", X: 3}}).Body.Close()

	// Sanity: the same-doc stream must receive it.
	gotA := streamA.waitFor(func(ev string) bool { return shapesEventContains([]string{ev}, "only-a") })
	if !shapesEventContains(gotA, "only-a") {
		t.Fatalf("same-doc peer never received the shape; events=%s", tailEvents(gotA))
	}

	// The other-doc stream must NOT receive docA's shape.
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if evs := streamB.drain(50 * time.Millisecond); shapesEventContains(evs, "only-a") {
			t.Fatalf("cross-doc leak: docB stream received docA shape; events=%s", tailEvents(evs))
		}
	}
}
