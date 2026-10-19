// SCOPE:feature — acceptance: anonymous callers can neither read nor write a board.

package handlers

import (
	"context"
	"net/http"
	"testing"

	"github.com/calionauta/gogogo/internal/collab"
)

// TestWhiteboardRejectsAnonymousAccess is the acceptance gate for
// "shared boards are open to every SIGNED-IN user".
//
// Before the gate, a cookie-less client drew into any board id it could
// guess (POST /api/whiteboard/{id}/update answered 200) and read the
// whole board body back from /snapshot. RBAC is opt-in, so the handler
// owns the rule.
func TestWhiteboardRejectsAnonymousAccess(t *testing.T) {
	server, mem, _ := wbPortFixture(t)
	anon := &http.Client{}
	docID := "doc-anon-gate"

	// 1. Write.
	resp := postOp(t, anon, server, docID, "anonW", collab.ShapeOp{
		Op: "add", Shape: collab.Shape{ID: "s1", Type: "rect", X: 0},
	})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anon update status = %d, want 401", resp.StatusCode)
	}
	if _, ok := mem.LoadSnapshot(docID); ok {
		t.Error("anon update persisted a snapshot: anonymous write reached the store")
	}

	// 2. Read (snapshot IS the board body).
	sreq, serr := http.NewRequestWithContext(t.Context(), http.MethodGet,
		server.URL+"/api/whiteboard/"+docID+"/snapshot", nil)
	if serr != nil {
		t.Fatalf("anon snapshot request: %v", serr)
	}
	sresp, serr := anon.Do(sreq)
	if serr != nil {
		t.Fatalf("anon snapshot transport: %v", serr)
	}
	_ = sresp.Body.Close()
	if sresp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anon snapshot status = %d, want 401", sresp.StatusCode)
	}

	// 3. Read (the stream carries shapes + peers).
	stresp, sterr := anon.Do(mustGet(t, server.URL+"/api/whiteboard/"+docID+"/stream?clientID=anonR"))
	if sterr != nil {
		t.Fatalf("anon stream transport: %v", sterr)
	}
	_ = stresp.Body.Close()
	if stresp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anon stream status = %d, want 401", stresp.StatusCode)
	}

	// 4. Presence.
	presp, perr := postWithClientID(context.Background(), anon,
		server.URL+"/api/whiteboard/"+docID+"/presence", "anonP",
		[]byte(`{"type":"cursor","x":1,"y":2}`))
	if perr != nil {
		t.Fatalf("anon presence transport: %v", perr)
	}
	_ = presp.Body.Close()
	if presp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anon presence status = %d, want 401", presp.StatusCode)
	}
}

// TestWhiteboardAllowsSignedInDrawer pins the other half: a real session
// still draws on a board it did not create (shared by design).
func TestWhiteboardAllowsSignedInDrawer(t *testing.T) {
	server, mem, cookie := wbPortFixture(t)
	client := wbClient(t, server, cookie)
	docID := "doc-drawer-ok"

	resp := postOp(t, client, server, docID, "d1", collab.ShapeOp{
		Op: "add", Shape: collab.Shape{ID: "s1", Type: "rect", X: 0},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("signed-in update status = %d, want 200", resp.StatusCode)
	}
	if _, ok := mem.LoadSnapshot(docID); !ok {
		t.Error("signed-in update did not persist a snapshot")
	}
}
