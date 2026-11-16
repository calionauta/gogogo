// Generated-query contracts for the reference slice.
//
// These live in the slice, not in internal/db: the flat query layer is core,
// but the SQL they exercise belongs to `features/todo`. A test that named the
// slice from `internal/db` kept the slice undeletable — the same class of
// coupling the desktopboot migration set had.
package service_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// newStore is a migrated temp database: core schema plus this unit's own DDL.
func newStore(t *testing.T) *dbgen.Queries {
	t.Helper()
	sqlDB, err := db.OpenAt(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("Migrate: %v", merr)
	}
	set := dbschema.MigrationSet{ID: "todo", FS: db.RecipeSchema("todo")}
	if merr := dbschema.MigrateWith(sqlDB, set); merr != nil {
		t.Fatalf("todo migrate: %v", merr)
	}
	return dbgen.New(sqlDB)
}

func createUser(t *testing.T, q *dbgen.Queries, email string) dbgen.User {
	t.Helper()
	now := dbschema.Now()
	u, err := q.CreateUser(context.Background(), dbgen.CreateUserParams{
		ID:           dbschema.NewID(),
		Email:        email,
		PasswordHash: "hash",
		Name:         "Demo",
		Created:      now,
		Updated:      now,
	})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", email, err)
	}
	return u
}

// TestGeneratedTodoQueries covers the reference slice's SQL end to end: create,
// list in reverse-chronological order, toggle (bool round-trip through the
// INTEGER column), owner-scoped update, count and delete.
func TestGeneratedTodoQueries(t *testing.T) {
	q := newStore(t)
	ctx := context.Background()
	owner := createUser(t, q, "demo1@demo.app").ID
	other := createUser(t, q, "demo2@demo.app").ID

	ids := make([]string, 0, 2)
	for _, title := range []string{"first", "second"} {
		todo, err := q.CreateTodo(ctx, dbgen.CreateTodoParams{
			ID: dbschema.NewID(), Owner: owner, Title: title,
			Completed: false, IdemKey: "", Visibility: "private",
			Created: dbschema.Now(), Updated: dbschema.Now(),
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
		Completed: true, Updated: dbschema.Now(), ID: ids[0], Owner: owner,
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
		Completed: false, Updated: dbschema.Now(), ID: ids[0], Owner: other,
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
