// SCOPE:feature — acceptance: opening a fresh board id registers it, so it can take writes.

package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/queue"
)

// TestWhiteboardOpenRegistersFreshId is the contract the registry check must
// not break: opening a board id nobody created yet WORKS, and the first
// signed-in visitor becomes its author, so the very next draw persists.
//
// This is exactly what scripts/whiteboard-client.test.mjs does (invented id
// "wb-order-<pid>", open the page, draw twice, then assert the server holds
// two shapes) and what a shared link does. The registry check shipped without
// this and CI caught it: "server has 0 shapes, want 2" — the write answered
// 404 because no row existed and nothing had created one.
//
// Uses the PRODUCTION registry on purpose (no fixture opt-out), so a
// regression in claimDoc fails here instead of in a browser run.
func TestWhiteboardOpenRegistersFreshId(t *testing.T) {
	server, mem, cookie := wbProductionFixture(t)
	client := wbClient(t, server, cookie)
	fresh := "wb-order-75843" // shaped like the browser suite's id

	// 1. Open the page for an id nobody registered.
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		server.URL+"/whiteboard/"+fresh, nil)
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	resp, derr := client.Do(req)
	if derr != nil {
		t.Fatalf("open transport: %v", derr)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("opening a fresh board id = %d, want 200", resp.StatusCode)
	}

	// 2. The draw on it must be accepted and persisted.
	op := postOp(t, client, server, fresh, "d1", collab.ShapeOp{
		Op: "add", Shape: collab.Shape{ID: "s1", Type: "rect", X: 0},
	})
	_ = op.Body.Close()
	if op.StatusCode != http.StatusOK {
		t.Errorf("draw on a just-opened board = %d, want 200 (claimDoc did not register it)", op.StatusCode)
	}
	if _, ok := mem.LoadSnapshot(fresh); !ok {
		t.Error("draw on a just-opened board persisted nothing")
	}
}

// TestWhiteboardWriteToNeverOpenedIdStillRefused is the other half: the gate
// still holds for an id that was never opened. Without this, claimDoc could be
// widened into "upsert on write" and re-open the orphan.
func TestWhiteboardWriteToNeverOpenedIdStillRefused(t *testing.T) {
	server, mem, cookie := wbProductionFixture(t)
	client := wbClient(t, server, cookie)
	ghost := "wb-never-opened"

	op := postOp(t, client, server, ghost, "d1", collab.ShapeOp{
		Op: "add", Shape: collab.Shape{ID: "s1", Type: "rect", X: 0},
	})
	_ = op.Body.Close()
	if op.StatusCode != http.StatusNotFound {
		t.Errorf("write to a never-opened id = %d, want 404", op.StatusCode)
	}
	if _, ok := mem.LoadSnapshot(ghost); ok {
		t.Error("write to a never-opened id persisted a snapshot (the orphan is back)")
	}
}

// wbProductionFixture builds the handler WITHOUT the transport opt-out, so the
// real registry (the boards table) decides.
func wbProductionFixture(t *testing.T) (*httptest.Server, *collab.MemoryPersister, *http.Cookie) {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/claim.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("db migrate: %v", merr)
	}
	a, aerr := auth.New(dbgen.New(sqlDB), false)
	if aerr != nil {
		t.Fatalf("auth new: %v", aerr)
	}
	mem := collab.NewMemoryPersister()
	h := NewHandler(dbgen.New(sqlDB), a, queue.NewSSEHub(), mem,
		collab.NewDocStore(), config.Load())
	// NOTE: h.registry intentionally left nil — production behaviour.
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Routes())
	mux.Handle("/", h.Routes())
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	cookie := wbSignup(t, mux, "claim@example.com")
	return server, mem, cookie
}
