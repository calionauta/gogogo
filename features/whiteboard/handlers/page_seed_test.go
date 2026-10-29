// SCOPE:feature — the board page seeds the canvas with the server's shapes.
package handlers

import (
	"net/http"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/collab"
)

// TestBoardPageSeedsShapes proves an offline reload can paint the board: the
// page carries the resolved shape list in `data-shapes`, so the canvas has
// something to render before (or without) the SSE stream reconnecting. The
// stream remains the live channel; this is the snapshot the page already has.
//
// Red-proof: drop the data-shapes attribute (or the marshal in handleBoard) and
// the assertion fails while the stream-based tests stay green.
func TestBoardPageSeedsShapes(t *testing.T) {
	server, _, cookie := wbPortFixture(t)
	client := wbClient(t, server, cookie)
	docID := "seedboard"

	// Opening the board claims it (the write path requires the row).
	_ = wbRead(t, client, server.URL+"/whiteboard/"+docID)

	resp := postOp(t, client, server, docID, "tab-seed", collab.ShapeOp{
		Op: "add",
		Shape: collab.Shape{
			ID: "shape-seed-1", Type: "rect", X: 1, Y: 2, W: 3, H: 4, Color: "#111111",
		},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post shape: status = %d", resp.StatusCode)
	}

	page := wbRead(t, client, server.URL+"/whiteboard/"+docID)
	if !strings.Contains(page, "data-shapes=") {
		t.Fatal("board page has no data-shapes seed")
	}
	// The seed is a RENDER-time snapshot, so the offline canvas can date it.
	if !strings.Contains(page, "data-seeded-at=") {
		t.Fatal("board page seed has no render time")
	}
	if !strings.Contains(page, "shape-seed-1") {
		t.Fatal("board page seed does not carry the stored shape")
	}
}
