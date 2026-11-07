// The todo unit owns its table now: the conversion's proof.
//
// Two properties hold it together: a database that already holds rows in the
// legacy core `todos` table carries every row into the owned table when the
// unit's migration set applies (an upgrade must not lose the user's list), and
// a second apply is a no-op (the ledger records the set under the unit's own
// namespace).
package handlers

import (
	"context"
	"testing"

	"github.com/calionauta/gogogo/features/todo"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/recipe"
)

func TestTodoMigrationCarriesLegacyRowsIntoOwnedTable(t *testing.T) {
	t.Parallel()
	sqlDB, err := db.OpenAt(t.TempDir() + "/legacy.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	// Core only: this is the database as it existed before the conversion.
	if cerr := dbschema.Migrate(sqlDB); cerr != nil {
		t.Fatalf("core migrate: %v", cerr)
	}
	owner, legacyID, now := dbschema.NewID(), dbschema.NewID(), dbschema.Now()
	if _, xerr := sqlDB.ExecContext(context.Background(),
		`INSERT INTO users (id, email, password_hash, name, created, updated)
		 VALUES (?, ?, ?, ?, ?, ?)`, owner, "legacy@example.com", "hash", "Legacy", now, now); xerr != nil {
		t.Fatalf("seed user: %v", xerr)
	}
	if _, lerr := sqlDB.ExecContext(context.Background(),
		`INSERT INTO todos (id, owner, title, completed, idem_key, visibility, created, updated)
		 VALUES (?, ?, ?, 0, '', 'public', ?, ?)`,
		legacyID, owner, "legacy row from before the split", now, now); lerr != nil {
		t.Fatalf("seed legacy row: %v", lerr)
	}

	set := dbschema.MigrationSet{ID: RecipeID, FS: todo.Migrations, Dir: "migrations"}
	if merr := dbschema.MigrateWith(sqlDB, set); merr != nil {
		t.Fatalf("todo migrate: %v", merr)
	}
	got, err := dbgen.New(sqlDB).GetTodo(context.Background(),
		dbgen.GetTodoParams{ID: legacyID, Owner: owner})
	if err != nil {
		t.Fatalf("legacy row did not cross into the owned table: %v", err)
	}
	if got.Title != "legacy row from before the split" || got.Visibility != "public" {
		t.Fatalf("carried row changed in transit: %+v", got)
	}
	var version string
	if err := sqlDB.QueryRowContext(context.Background(),
		`SELECT version FROM schema_migrations WHERE version = 'todo/0001_init'`).Scan(&version); err != nil {
		t.Fatalf("ledger must record the unit's namespace: %v", err)
	}
	// A second apply is a no-op, not a duplicate-key error on the copy.
	if secondErr := dbschema.MigrateWith(sqlDB, set); secondErr != nil {
		t.Fatalf("second apply must be a no-op, got %v", secondErr)
	}
}

func TestTodoDeclarationAndDDLAgree(t *testing.T) {
	t.Parallel()
	f := newHandlersFixture(t)
	provider := f.h.Recipe()
	if err := recipe.ValidateSchema(RecipeID, provider.Declaration(), todo.Migrations); err != nil {
		t.Fatalf("the todo declaration and its DDL must agree: %v", err)
	}
	if got := provider.Declaration().Tables; len(got) != 1 || got[0] != "ext_todo_todos" {
		t.Fatalf("the todo declaration must name the owned table, got %v", got)
	}
}
