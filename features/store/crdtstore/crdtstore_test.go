// SCOPE:plugin - E2E test for CRDTStore.
//
// Boots a temp PocketBase app (like db/idempotency_hook_test.go),
// runs EnsureSchema, then exercises the 7 EntityStore methods
// against the CRDTStore. Asserts:
//   - the `todos` collection is created with the expected fields
//   - Create returns the persisted entity; Get retrieves it
//   - Update flips `completed` and bumps `updated`
//   - Delete returns ErrNotFound on a second call
//   - List with "active" / "completed" filters correctly
//   - ClearCompleted removes only completed items
//   - Count matches List length
//   - record round-trips: write → load from a fresh CRDTStore on the
//     same data dir → state matches (this is the offline-replay
//     / restart scenario)
//
// CRDTStore projects todos as normal PocketBase `todos` records with
// `owner` as a relation to _pb_users_auth_, so every test uses a real
// auth user id as the owner (see newTestUser) and PB-compatible record
// ids (alphanumeric, <=15 chars, matching PocketBase's id pattern).
package crdtstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"

	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/calionauta/gogogo/features/store"
	"github.com/calionauta/gogogo/features/todo"
)

// userSeq gives every test user a unique email.
var userSeq atomic.Int64

// newTestUser creates a real auth user in app and returns its id.
// CRDTStore writes `todos` with owner as a relation to
// _pb_users_auth_, so tests must use a valid user id (not an arbitrary
// string) as the owner.
func newTestUser(t *testing.T, app *pocketbase.PocketBase) string {
	t.Helper()
	return newTestUserWithID(t, app, "")
}

func newTestUserWithID(t *testing.T, app *pocketbase.PocketBase, id string) string {
	t.Helper()
	users, err := app.FindCollectionByNameOrId("_pb_users_auth_")
	if err != nil {
		t.Fatalf("find users collection: %v", err)
	}
	rec := core.NewRecord(users)
	if id != "" {
		rec.Set("id", id)
	}
	rec.Set("email", fmt.Sprintf("test-%d@example.com", userSeq.Add(1)))
	rec.Set("password", "password123")
	if err := app.Save(rec); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return rec.Id
}

// newTestUserInBoth creates a user with the SAME id in two separate
// PocketBase instances (used by cross-instance transport tests where
// both stores must agree on the owner id).
func newTestUserInBoth(t *testing.T, appA, appB *pocketbase.PocketBase) string {
	t.Helper()
	id := newTestUser(t, appA)
	newTestUserWithID(t, appB, id)
	return id
}

// newTestApp is a local copy of the same pattern db/seed_test.go uses
// (boots a fresh PocketBase app on a temp dir with the same ncruces
// driver as production). Duplicated here rather than exported from db
// to keep the test fixture local; if a third package needs it,
// promote to internal/testutil.
//
// NOTE: the DSN MUST use the "file:" URI prefix. ncruces only parses
// ?_pragma= query params when the DSN is a file: URI; without the prefix
// it treats the whole string (path + "?_pragma=...") as a filename and
// SILENTLY DROPS every pragma (journal_mode stays DELETE, foreign_keys
// OFF). This matches production's DBConnect in db/pocketbase.go.
func newTestApp(t *testing.T, tmpDir string) *pocketbase.PocketBase {
	t.Helper()
	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir: tmpDir,
		DBConnect: func(dbPath string) (*dbx.DB, error) {
			pragmas := "?_pragma=busy_timeout(10000)" +
				"&_pragma=journal_mode(WAL)" +
				"&_pragma=foreign_keys(ON)"
			return dbx.Open("sqlite3", "file:"+dbPath+pragmas)
		},
	})
	if err := app.Bootstrap(); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	return app
}

func newCRDTStore(t *testing.T) (*CRDTStore, *pocketbase.PocketBase, func()) {
	t.Helper()
	tmpDir, mkErr := os.MkdirTemp("", "crdtstore-*")
	if mkErr != nil {
		t.Fatal(mkErr)
	}
	cleanup := func() { os.RemoveAll(tmpDir) }

	app := newTestApp(t, tmpDir)
	s := New(app)
	if schemaErr := s.EnsureSchema(); schemaErr != nil {
		cleanup()
		t.Fatalf("EnsureSchema: %v", schemaErr)
	}
	return s, app, cleanup
}

func TestCRDTStore_EnsureSchemaCreatesCollection(t *testing.T) {
	t.Parallel()
	s, _, cleanup := newCRDTStore(t)
	defer cleanup()
	if err := s.EnsureSchema(); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	col, err := s.app.FindCollectionByNameOrId(todosCollectionName)
	if err != nil {
		t.Fatalf("collection %q not found: %v", todosCollectionName, err)
	}
	for _, f := range []string{"title", "completed", "owner", "created", "updated", "idem_key"} {
		if col.Fields.GetByName(f) == nil {
			t.Errorf("field %q missing", f)
		}
	}
}

func TestCRDTStore_CreateGetListUpdateDelete(t *testing.T) {
	t.Parallel()
	s, app, cleanup := newCRDTStore(t)
	defer cleanup()
	ctx := context.Background()
	owner := newTestUser(t, app)

	// Create: client-generated ID (PB-compatible); store fills timestamps.
	id := "111111112222333"
	in := todo.Todo{ID: id, Title: "first", Completed: false}
	out, err := s.Create(ctx, in, owner, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if out.ID != id || out.Title != "first" || out.Completed {
		t.Errorf("Create returned %+v, want id+title+!completed", out)
	}
	if out.CreatedAt.IsZero() {
		t.Error("Create did not set CreatedAt")
	}
	if out.UpdatedAt.IsZero() {
		t.Error("Create did not set UpdatedAt")
	}

	// Get: round-trip.
	got, err := s.Get(ctx, owner, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "first" {
		t.Errorf("Get title = %q, want %q", got.Title, "first")
	}

	// Get cross-owner: ErrNotFound.
	if _, getErr := s.Get(ctx, "other-owner", id); !errors.Is(getErr, store.ErrNotFound) {
		t.Errorf("cross-owner Get err = %v, want ErrNotFound", getErr)
	}

	// Update: toggle completed. The new UpdatedAt must be strictly
	// later than the original — we don't compare wall clock strings
	// directly (RFC3339 sub-second precision can collapse under
	// record round-trips); we just assert the timestamp advanced.
	updated, err := s.Update(ctx, owner, id, map[string]any{"completed": true, "title": "first-edited"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !updated.Completed || updated.Title != "first-edited" {
		t.Errorf("Update returned %+v", updated)
	}
	if !updated.UpdatedAt.After(out.UpdatedAt) {
		t.Errorf("UpdatedAt did not advance: %v vs %v", updated.UpdatedAt, out.UpdatedAt)
	}

	// List: all, active, completed.
	all, err := s.List(ctx, owner, "")
	if err != nil || len(all) != 1 {
		t.Errorf("List all: err=%v len=%d, want 1", err, len(all))
	}
	active, _ := s.List(ctx, owner, "active")
	completed, _ := s.List(ctx, owner, "completed")
	if len(active) != 0 {
		t.Errorf("active filter returned %d, want 0", len(active))
	}
	if len(completed) != 1 {
		t.Errorf("completed filter returned %d, want 1", len(completed))
	}

	// Count matches List length.
	if c, _ := s.Count(ctx, owner); c != 1 {
		t.Errorf("Count = %d, want 1", c)
	}

	// Delete: removes.
	if err := s.Delete(ctx, owner, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, owner, id); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("after Delete, Get err = %v, want ErrNotFound", err)
	}
	// Second delete: ErrNotFound (idempotent path).
	if err := s.Delete(ctx, owner, id); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("second Delete err = %v, want ErrNotFound", err)
	}
}

func TestCRDTStore_ClearCompleted(t *testing.T) {
	t.Parallel()
	s, app, cleanup := newCRDTStore(t)
	defer cleanup()
	ctx := context.Background()
	owner := newTestUser(t, app)

	// Create 3: 2 completed, 1 active.
	for i, title := range []string{"a", "b", "c"} {
		id := []string{"ida", "idb", "idc"}[i]
		_, err := s.Create(ctx, todo.Todo{ID: id, Title: title}, owner, "")
		if err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	// Mark ida and idb completed.
	if _, err := s.Update(ctx, owner, "ida", map[string]any{"completed": true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(ctx, owner, "idb", map[string]any{"completed": true}); err != nil {
		t.Fatal(err)
	}

	n, err := s.ClearCompleted(ctx, owner)
	if err != nil {
		t.Fatalf("ClearCompleted: %v", err)
	}
	if n != 2 {
		t.Errorf("ClearCompleted returned %d, want 2", n)
	}
	all, _ := s.List(ctx, owner, "")
	if len(all) != 1 || all[0].ID != "idc" {
		t.Errorf("after Clear, list = %+v, want only idc", all)
	}
}

func TestCRDTStore_RecordRoundTrip(t *testing.T) {
	t.Parallel()
	// Root cause (found via investigation spikes in /spikes, gitignored):
	// the write path keys the Loro items map by the TODO id (= idem_key,
	// the client-generated id). Two read/prune paths instead used the
	// PocketBase ROW id (r.Id), a different namespace, producing a key
	// mismatch:
	//   1) persistRecords' delete-stale loop checked `want[rec.Id]`;
	//      want is keyed by todo id, so the check was always false and
	//      EVERY just-upserted record was immediately deleted -> DB held
	//      0 rows -> the round-trip (reload -> doc() rebuild -> List)
	//      returned nothing. Other CRUD tests passed only because they
	//      read the in-memory Loro doc, never the DB.
	//   2) doc()'s reload keyed the Loro map by r.Id instead of
	//      idem_key -> after a restart/reload, Get/Lookup-by-todo-id
	//      failed and List returned todos whose IDs were PB row ids.
	// Fixes (crdtstore.go):
	//   - delete loop: `want[rec.GetString("idem_key")]`
	//   - doc() reload: `items.InsertMapContainer(r.GetString("idem_key"), ...)`
	// Re-enable this test now that the mismatch is fixed (previously
	// skipped while the cause was still unknown).
	// CRDTStore projects todos as normal `todos` records. A fresh
	// CRDTStore on the SAME PocketBase app (simulating an in-process
	// store restart) must rebuild its in-memory doc from those records.
	// This is the offline-replay / restart scenario.
	tmpDir, mkdirErr := os.MkdirTemp("", "crdtstore-roundtrip-*")
	if mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	defer os.RemoveAll(tmpDir)

	app := newTestApp(t, tmpDir)
	s1 := New(app)
	if schemaErr := s1.EnsureSchema(); schemaErr != nil {
		t.Fatal(schemaErr)
	}
	ctx := context.Background()
	owner := newTestUser(t, app)
	for i, title := range []string{"alpha", "beta", "gamma"} {
		id := []string{"idalp", "idbet", "idgam"}[i]
		if _, createErr := s1.Create(ctx, todo.Todo{ID: id, Title: title}, owner, ""); createErr != nil {
			t.Fatal(createErr)
		}
	}
	if _, updateErr := s1.Update(ctx, owner, "idbet", map[string]any{"completed": true}); updateErr != nil {
		t.Fatal(updateErr)
	}

	// Simulate a restart: clear the in-memory CRDT state. The
	// `todos` records survive in SQLite, so a brand-new store on the
	// same app must rebuild its in-memory doc from them.
	_ = s1.Close()
	s2 := New(app)
	all, err := s2.List(ctx, owner, "")
	if err != nil {
		t.Fatalf("s2.List: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("s2.List returned %d items, want 3", len(all))
	}
	gotBeta, _ := s2.Get(ctx, owner, "idbet")
	if !gotBeta.Completed {
		t.Errorf("s2: idbet Completed = false, want true (records not restored)")
	}
}

func TestCRDTStore_EmptyOwnerReturnsEmpty(t *testing.T) {
	t.Parallel()
	s, _, cleanup := newCRDTStore(t)
	defer cleanup()
	ctx := context.Background()
	if all, _ := s.List(ctx, "never-touched-owner", ""); len(all) != 0 {
		t.Errorf("List on fresh owner returned %d, want 0", len(all))
	}
	if c, _ := s.Count(ctx, "never-touched-owner"); c != 0 {
		t.Errorf("Count on fresh owner = %d, want 0", c)
	}
}

func TestCRDTStore_WatchSignals(t *testing.T) {
	t.Parallel()
	s, app, _ := newCRDTStore(t)
	ctx := context.Background()
	ownerID := newTestUser(t, app)
	if _, err := s.Create(ctx, todo.Todo{ID: "watch1", Title: "first"}, ownerID, ""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	ch, cancel := s.Watch(ownerID)
	defer cancel()
	// Watch sends current value immediately.
	select {
	case v := <-ch:
		if v < 1 {
			t.Errorf("initial event v=%d, want >= 1", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive initial event")
	}
	// Next event triggered by a new mutation.
	if _, err := s.Create(ctx, todo.Todo{ID: "watch2", Title: "second"}, ownerID, ""); err != nil {
		t.Fatalf("Create 2: %v", err)
	}
	select {
	case v := <-ch:
		if v < 2 {
			t.Errorf("second event v=%d, want >= 2", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive second event")
	}
}
