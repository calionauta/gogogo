// SCOPE:feature — acceptance: a draw on an unregistered board id is refused.

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

// TestWhiteboardRefusesDrawOnUnregisteredDoc is the acceptance gate for the
// storage half: a draw on an id with no metadata row answers 404 and
// persists nothing.
//
// This fixture does NOT clear h.registry, unlike the transport fixtures, so
// it exercises the production default a real request hits.
func TestWhiteboardRefusesDrawOnUnregisteredDoc(t *testing.T) {
	sqlDB, err := db.OpenAt(t.TempDir() + "/registry.db")
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
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Handler())
	mux.Handle("/", h.Routes())
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	cookie := wbSignup(t, mux, "wregistry@example.com")
	client := wbClient(t, server, cookie)

	ghost := "doc-ghost-unregistered"
	resp := postOp(t, client, server, ghost, "g1", collab.ShapeOp{
		Op: "add", Shape: collab.Shape{ID: "s1", Type: "rect", X: 0},
	})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("draw on unregistered board: status = %d, want 404", resp.StatusCode)
	}
	if _, ok := mem.LoadSnapshot(ghost); ok {
		t.Error("draw on unregistered board persisted a snapshot (the orphan gap is back)")
	}
}
