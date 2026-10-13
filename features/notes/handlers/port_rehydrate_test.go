// SCOPE:feature — Notes snapshot-rehydrate red-proof.
//
// The DocStore cache is bounded: after an evict (or a server restart)
// the next op for a doc must NOT start from an empty doc, or the
// resolved snapshot is persisted over the good one and every character
// written before is lost. The handler must wire its persister into the
// store so a cache miss rehydrates before the op applies.
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

// TestNotesOpAfterEvictKeepsText pins that an op arriving after the doc
// left the cache merges onto the persisted text instead of replacing it.
func TestNotesOpAfterEvictKeepsText(t *testing.T) {
	sqlDB, err := db.OpenAt(t.TempDir() + "/notes-evict.db")
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
	docs := collab.NewDocStore()
	h := NewHandler(dbgen.New(sqlDB), a, queue.NewSSEHub(), mem, docs, config.Load())
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Handler())
	mux.Handle("/", h.Routes())
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	cookie := notesSignup(t, mux, "evict@example.com")
	client := notesAuthedClient(t, server, cookie)
	docID := "note-evict"

	postOp(t, client, server, docID, "cliA", `{"base":0,"ops":[{"t":"ins","i":0,"s":"hello"}]}`)

	// The doc leaves the cache (eviction, restart). The snapshot stays.
	docs.Evict(docID)

	code, out := postOp(t, client, server, docID, "cliA", `{"base":1,"ops":[{"t":"ins","i":5,"s":" world"}]}`)
	if code != http.StatusOK {
		t.Fatalf("post-evict op: status = %d: %v", code, out)
	}
	if out["text"] != "hello world" {
		t.Fatalf("post-evict op lost persisted text: text = %v, want %q", out["text"], "hello world")
	}
}
