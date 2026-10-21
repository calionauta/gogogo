// SCOPE:feature — acceptance: opening a fresh note id registers it, so it can take writes.

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

// TestNotesOpenRegistersFreshId is the notes half of the same contract as the
// whiteboard test: opening a note id nobody created yet works, registers the
// visitor as author, and the next op persists.
//
// scripts/notes-caret.test.mjs drives exactly this shape (invented ids
// "notes-caret-<pid>", "notes-sameuser-<pid>", opened by two tabs).
func TestNotesOpenRegistersFreshId(t *testing.T) {
	server, mem, cookie := notesProductionFixture(t)
	client := notesAuthedClient(t, server, cookie)
	fresh := "notes-caret-75843"

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		server.URL+"/notes/"+fresh, nil)
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	resp, derr := client.Do(req)
	if derr != nil {
		t.Fatalf("open transport: %v", derr)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("opening a fresh note id = %d, want 200", resp.StatusCode)
	}

	code, _, perr := postOpRaw(client, server, fresh, "c1",
		`{"base":0,"ops":[{"t":"ins","i":0,"s":"hi"}]}`)
	if perr != nil {
		t.Fatalf("op transport: %v", perr)
	}
	if code != http.StatusOK {
		t.Errorf("op on a just-opened note = %d, want 200 (claimDoc did not register it)", code)
	}
	if _, ok := mem.LoadSnapshot(fresh); !ok {
		t.Error("op on a just-opened note persisted nothing")
	}
}

// TestNotesWriteToNeverOpenedIdStillRefused keeps the gate honest: claimDoc
// registers on OPEN, never on write, so the orphan cannot come back.
func TestNotesWriteToNeverOpenedIdStillRefused(t *testing.T) {
	server, mem, cookie := notesProductionFixture(t)
	client := notesAuthedClient(t, server, cookie)
	ghost := "notes-never-opened"

	code, _, perr := postOpRaw(client, server, ghost, "c1",
		`{"base":0,"ops":[{"t":"ins","i":0,"s":"hi"}]}`)
	if perr != nil {
		t.Fatalf("op transport: %v", perr)
	}
	if code != http.StatusNotFound {
		t.Errorf("op on a never-opened id = %d, want 404", code)
	}
	if _, ok := mem.LoadSnapshot(ghost); ok {
		t.Error("op on a never-opened id persisted a snapshot (the orphan is back)")
	}
}

// notesProductionFixture builds the handler WITHOUT the transport opt-out.
func notesProductionFixture(t *testing.T) (*httptest.Server, *collab.MemoryPersister, *http.Cookie) {
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
	cookie := notesSignup(t, mux, "nclaim@example.com")
	return server, mem, cookie
}
