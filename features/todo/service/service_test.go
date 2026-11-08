// Domain service contracts: one rule, every frontend.
package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/calionauta/gogogo/features/todo"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

type fakeOnboarding struct{ calls atomic.Int32 }

func (f *fakeOnboarding) ResumeOnboarding(string) { f.calls.Add(1) }

func openTestDB(tb testing.TB) (*dbgen.Queries, *Service, *fakeOnboarding, string) {
	tb.Helper()
	sqlDB, err := db.OpenAt(tb.TempDir() + "/svc.db")
	if err != nil {
		tb.Fatalf("db open: %v", err)
	}
	tb.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		tb.Fatalf("migrate: %v", merr)
	}
	todoSet := dbschema.MigrationSet{ID: "todo", FS: todo.Migrations, Dir: "migrations"}
	if merr := dbschema.MigrateWith(sqlDB, todoSet); merr != nil {
		tb.Fatalf("todo migrate: %v", merr)
	}
	q := dbgen.New(sqlDB)
	a, err := auth.New(q, false)
	if err != nil {
		tb.Fatalf("auth: %v", err)
	}
	user, tok, err := a.Signup(context.Background(), "svc@example.com", "password123", "Svc")
	if err != nil {
		tb.Fatalf("signup: %v", err)
	}
	_ = user
	sess, err := a.SessionByToken(context.Background(), tok)
	if err != nil {
		tb.Fatalf("session: %v", err)
	}
	owner := sess.User.ID
	var events []todo.Event
	bus := todo.NewBus(nil, "")
	bus.SetWire(nil)
	// capture via Subscribe (in-process subscribers)
	cancel := bus.Subscribe(func(ev todo.Event) { events = append(events, ev) })
	tb.Cleanup(cancel)
	ob := &fakeOnboarding{}
	svc := New(q, nil, sqlDB, bus, ob)
	_ = bus
	return q, svc, ob, owner
}

func TestCreateOnboardingAndPublish(t *testing.T) {
	_, svc, ob, owner := openTestDB(t)
	ctx := context.Background()
	id, err := svc.Create(ctx, owner, "Buy milk", todo.VisibilityPrivate, "idem-1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == "" {
		t.Fatal("empty id")
	}
	if ob.calls.Load() != 1 {
		t.Fatalf("onboarding calls = %d, want 1", ob.calls.Load())
	}
	// idempotent replay
	id2, err := svc.Create(ctx, owner, "Buy milk", todo.VisibilityPrivate, "idem-1")
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if id2 != id {
		t.Fatalf("replay id = %q, want %q", id2, id)
	}
	list, err := svc.ListOwned(ctx, owner, "")
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %d, %v; want 1", len(list), err)
	}
}

func TestToggleFreshReadAndDelete(t *testing.T) {
	q, svc, _, owner := openTestDB(t)
	ctx := context.Background()
	a2, err := auth.New(q, false)
	if err != nil {
		t.Fatalf("auth2: %v", err)
	}
	_, tok2, err := a2.Signup(ctx, "other@example.com", "password123", "Other")
	if err != nil {
		t.Fatalf("signup2: %v", err)
	}
	sess2, _ := a2.SessionByToken(ctx, tok2)
	otherOwner := sess2.User.ID
	id, err := svc.Create(ctx, owner, "Toggle me", "", "k1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// toggle with fresh read: flip to completed
	cur, applied, err := svc.Toggle(ctx, owner, id, "")
	if err != nil || !applied {
		t.Fatalf("toggle applied=%v err=%v", applied, err)
	}
	if cur.Completed {
		t.Fatalf("pre-toggle completed should be false")
	}
	list, _ := svc.ListOwned(ctx, owner, "")
	if len(list) != 1 || !list[0].Completed {
		t.Fatalf("after toggle = %+v, want completed", list)
	}
	// replay with same opKey must not flip back
	_, applied, err = svc.Toggle(ctx, owner, id, "op-1")
	if err != nil || !applied {
		t.Fatalf("first toggle op-1 applied=%v err=%v", applied, err)
	}
	_, applied, err = svc.Toggle(ctx, owner, id, "op-1")
	if err != nil {
		t.Fatalf("replay toggle err: %v", err)
	}
	if applied {
		t.Fatal("replay toggle applied, want false (replayed)")
	}
	// viewer deletes OWN row: allowed (viewer restriction applies to
	// shared rows, never to own private todos — canDeleteRow rule).
	// viewer deletes ANOTHER user's shared row: forbidden.
	sharedID, err := svc.Create(ctx, otherOwner, "shared row", todo.VisibilityPublic, "k-shared")
	if err != nil {
		t.Fatalf("create shared: %v", err)
	}
	if _, err = svc.Delete(ctx, owner, "viewer", sharedID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer delete shared err = %v, want ErrForbidden", err)
	}
	// owner deletes own
	if _, err := svc.Delete(ctx, owner, "user", id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, _ = svc.ListOwned(ctx, owner, "")
	if len(list) != 0 {
		t.Fatalf("after delete = %d, want 0", len(list))
	}
}

func TestValidation(t *testing.T) {
	_, svc, _, owner := openTestDB(t)
	ctx := context.Background()
	if _, err := svc.Create(ctx, owner, "", "", "k"); !errors.Is(err, ErrTitleRequired) {
		t.Fatalf("empty title err = %v, want ErrTitleRequired", err)
	}
	if _, err := svc.Create(ctx, owner, "x", "", "bad key!"); !errors.Is(err, ErrInvalidIdemKey) {
		t.Fatalf("bad idem err = %v, want ErrInvalidIdemKey", err)
	}
}
