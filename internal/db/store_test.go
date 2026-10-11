package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
)

// newStore is the fixture for every generated-query test: a migrated temp
// database and a *dbgen.Queries built the same way a handler will build it.
func newStore(t *testing.T) (*sql.DB, *dbgen.Queries) {
	t.Helper()
	sqlDB := openTestDB(t)
	return sqlDB, dbgen.New(sqlDB)
}

// createLimenOwner inserts one identity row and returns its integer id.
// Identity lives in limen_users (see 0003_identity): the app tables only
// ever reference it, never duplicate it.
func createLimenOwner(t *testing.T, sqlDB *sql.DB, email string) int64 {
	t.Helper()
	res, err := sqlDB.ExecContext(context.Background(),
		`INSERT INTO limen_users (email, created_at, updated_at) VALUES (?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		email,
	)
	if err != nil {
		t.Fatalf("insert limen_users(%s): %v", email, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("limen_users id: %v", err)
	}
	return id
}

// TestGeneratedTodoQueries covers the reference slice end to end: create,
// list in reverse-chronological order, toggle (bool round-trip through the
// INTEGER column), and owner-scoped delete.
func TestGeneratedTodoQueries(t *testing.T) {
	sqlDB, q := newStore(t)
	ctx := context.Background()
	owner := createLimenOwner(t, sqlDB, "demo1@demo.app")
	other := createLimenOwner(t, sqlDB, "demo2@demo.app")

	ids := make([]string, 0, 2)
	for _, title := range []string{"first", "second"} {
		todo, err := q.CreateTodo(ctx, dbgen.CreateTodoParams{
			ID: NewID(), Owner: owner, Title: title,
			Completed: false, IdemKey: "", Created: Now(), Updated: Now(),
		})
		if err != nil {
			t.Fatalf("CreateTodo(%q): %v", title, err)
		}
		if todo.Completed {
			t.Fatalf("new todo %q came back completed", title)
		}
		ids = append(ids, todo.ID)
	}

	list, err := q.ListTodosByOwner(ctx, owner)
	if err != nil {
		t.Fatalf("ListTodosByOwner: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListTodosByOwner = %d rows, want 2", len(list))
	}
	// Same created timestamp, so the id DESC tiebreak decides: the second
	// insert is the newer v7 uuid.
	if list[0].ID != ids[1] {
		t.Fatalf("list order = %s first, want newest (%s) first", list[0].ID, ids[1])
	}

	if serr := q.SetTodoCompleted(ctx, dbgen.SetTodoCompletedParams{
		Completed: true, Updated: Now(), ID: ids[0], Owner: owner,
	}); serr != nil {
		t.Fatalf("SetTodoCompleted: %v", serr)
	}
	got, err := q.GetTodo(ctx, dbgen.GetTodoParams{ID: ids[0], Owner: owner})
	if err != nil {
		t.Fatalf("GetTodo: %v", err)
	}
	if !got.Completed {
		t.Fatal("completed did not round-trip as true")
	}

	// Owner scoping: another tenant's id matches nothing, so the update is a
	// no-op and the read is a miss.
	if err := q.SetTodoCompleted(ctx, dbgen.SetTodoCompletedParams{
		Completed: false, Updated: Now(), ID: ids[0], Owner: other,
	}); err != nil {
		t.Fatalf("cross-owner update: %v", err)
	}
	if got, err := q.GetTodo(ctx, dbgen.GetTodoParams{ID: ids[0], Owner: other}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-owner read = %+v, %v; want ErrNoRows", got, err)
	}

	if n, err := q.CountTodosByOwner(ctx, other); err != nil || n != 0 {
		t.Fatalf("CountTodosByOwner(other) = %d, %v; want 0, nil", n, err)
	}

	if err := q.DeleteTodo(ctx, dbgen.DeleteTodoParams{ID: ids[0], Owner: owner}); err != nil {
		t.Fatalf("DeleteTodo: %v", err)
	}
	if n, err := q.CountTodosByOwner(ctx, owner); err != nil || n != 1 {
		t.Fatalf("after delete CountTodosByOwner = %d, %v; want 1, nil", n, err)
	}
}

// Sessions now belong to limen (limen_sessions, migration 0002): the
// create/lookup/prune/revoke contract is covered red-proofed in
// internal/auth against the same driver, so it is not duplicated here.

// TestOpenCreatesParentDir pins the boot behaviour a fresh clone depends on:
// `data/` does not exist yet, and Open has to create it before the engine
// tries to open a file inside it.
func TestOpenCreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", "app.db")
	sqlDB, err := OpenAt(path)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	if err := Migrate(sqlDB); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
}
