// SCOPE:feature — acceptance: a write to an unregistered doc id is refused.

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

// TestNotesRefusesWriteToUnregisteredDoc is the acceptance gate for the
// storage half of the gap docs/rbac.md used to record: an op for an id with
// no metadata row must answer 404 and persist NOTHING.
//
// Crucially this fixture does NOT clear h.registry — it exercises the
// production default, which is what a request hits. The transport fixtures
// opt out explicitly; this test proves the contract they opt out of is real.
func TestNotesRefusesWriteToUnregisteredDoc(t *testing.T) {
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
	cookie := notesSignup(t, mux, "registry@example.com")
	client := notesAuthedClient(t, server, cookie)

	// A signed-in client invents an id: no metadata row exists.
	ghost := "note-ghost-unregistered"
	code, _, perr := postOpRaw(client, server, ghost, "g1", `{"base":0,"ops":[{"t":"ins","i":0,"s":"orphan"}]}`)
	if perr != nil {
		t.Fatalf("ghost op transport: %v", perr)
	}
	if code != http.StatusNotFound {
		t.Errorf("write to unregistered doc: status = %d, want 404", code)
	}
	if _, ok := mem.LoadSnapshot(ghost); ok {
		t.Error("write to unregistered doc persisted a snapshot (the orphan gap is back)")
	}

	// The registered path still works: create through the real route, then write.
	created := createNoteForTest(t, client, server)
	code, _, werr := postOpRaw(client, server, created, "g2", `{"base":0,"ops":[{"t":"ins","i":0,"s":"ok"}]}`)
	if werr != nil {
		t.Fatalf("registered op transport: %v", werr)
	}
	if code != http.StatusOK {
		t.Errorf("write to a registered doc: status = %d, want 200", code)
	}
	if _, ok := mem.LoadSnapshot(created); !ok {
		t.Error("registered write did not persist a snapshot")
	}
}
