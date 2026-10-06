// SCOPE:layer=feature,removal=feature — Collaborative whiteboard (Loro CRDT canvas)
// Depends on: internal/collab/ (CRDT).
//
// HTTP-level guards for the per-shape optimistic-concurrency contract, split out
// of web_test.go to stay within the 500-line budget.
package whiteboard_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/collab"
)

// TestWhiteboard_StaleOpReturns409NotSilentLoss is the end-to-end guard for the
// lost-update fix: a stale op must be answered with 409 plus the authoritative
// shape list, so the browser can resync instead of believing a lost edit stuck.
//
// Without versioning the server answered 200 and applied the stale write over
// the newer one, with no signal to either client.
func TestWhiteboard_StaleOpReturns409(t *testing.T) {
	t.Parallel()
	baseURL, _, cleanup := webFixture(t)
	defer cleanup()

	client := newWBClient(t)
	login(t, client, baseURL)

	docID := "doc-stale-" + time.Now().Format("150405.000")
	updURL := baseURL + "/api/whiteboard/" + docID + "/update"

	post := func(op collab.ShapeOp) *http.Response {
		t.Helper()
		body, mErr := json.Marshal(op)
		if mErr != nil {
			t.Fatalf("marshal: %v", mErr)
		}
		resp, err := postWithClientID(context.Background(), client, updURL, "wb1", body)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		return resp
	}

	// Draw: new shape, baseVersion 0.
	resp := post(collab.ShapeOp{Op: "add", Shape: collab.Shape{ID: "s", Type: "rect", X: 0}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initial draw status = %d, want 200", resp.StatusCode)
	}
	var okBody struct {
		Shapes []collab.Shape `json:"shapes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&okBody); err != nil {
		t.Fatalf("decode 200 body: %v", err)
	}
	resp.Body.Close()
	if len(okBody.Shapes) != 1 || okBody.Shapes[0].Version != 1 {
		t.Fatalf("200 body must carry the versioned shape, got %+v", okBody.Shapes)
	}

	// A peer edits against version 1 — accepted.
	resp = post(collab.ShapeOp{
		Op: "add", Shape: collab.Shape{ID: "s", Type: "rect", X: 99}, BaseVersion: 1,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("versioned edit status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// A stale op still based on version 1 — must be 409 with current state.
	resp = post(collab.ShapeOp{
		Op: "add", Shape: collab.Shape{ID: "s", Type: "rect", X: 10}, BaseVersion: 1,
	})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("stale op status = %d, want 409 (silent loss not fixed)", resp.StatusCode)
	}
	var conflict struct {
		Error  string         `json:"error"`
		Shapes []collab.Shape `json:"shapes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&conflict); err != nil {
		t.Fatalf("decode 409 body: %v", err)
	}
	resp.Body.Close()
	if conflict.Error != "stale" {
		t.Errorf("409 error = %q, want \"stale\"", conflict.Error)
	}
	if len(conflict.Shapes) != 1 || conflict.Shapes[0].X != 99 {
		t.Errorf("409 body must carry current state (x=99), got %+v", conflict.Shapes)
	}

	// The winning edit survived on the server.
	shapes := whiteboardShapes(t, baseURL, client, docID)
	if len(shapes) != 1 || shapes[0].X != 99 {
		t.Fatalf("peer's x=99 was clobbered by the stale op: %+v", shapes)
	}
}

// TestWhiteboard_DrawAfterClearSucceeds pins that clearing a board does not
// leave stale versions behind: drawing again must be accepted (baseVersion 0 on
// an id that no longer exists), not rejected as a conflict.
func TestWhiteboard_DrawAfterClearSucceeds(t *testing.T) {
	t.Parallel()
	baseURL, _, cleanup := webFixture(t)
	defer cleanup()

	client := newWBClient(t)
	login(t, client, baseURL)

	docID := "doc-cleardraw-" + time.Now().Format("150405.000")
	updURL := baseURL + "/api/whiteboard/" + docID + "/update"

	post := func(op collab.ShapeOp) int {
		t.Helper()
		body, _ := json.Marshal(op)
		resp, err := postWithClientID(context.Background(), client, updURL, "wb1", body)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if got := post(collab.ShapeOp{Op: "add", Shape: collab.Shape{ID: "s", Type: "rect"}}); got != http.StatusOK {
		t.Fatalf("draw status = %d, want 200", got)
	}
	if got := post(collab.ShapeOp{Op: "clear"}); got != http.StatusOK {
		t.Fatalf("clear status = %d, want 200", got)
	}
	if got := post(collab.ShapeOp{Op: "add", Shape: collab.Shape{ID: "s", Type: "rect", X: 3}}); got != http.StatusOK {
		t.Fatalf("draw after clear status = %d, want 200 (version leaked across clear)", got)
	}
}
