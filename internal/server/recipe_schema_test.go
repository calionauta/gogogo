// The schema-split gate: DDL and declaration must agree.
package server

import (
	"context"
	"database/sql"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/db"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/recipe"
)

// assertRecipeSchemas is the schema-split gate over the real composed tree.
//
// Two directions, one question: is the schema the same set as the declaration?
// A recipe that declares a table and ships no DDL asks the ledger for something
// nobody creates; a recipe that ships DDL for a table it never declared owns
// storage with no policy row, no uninstall story and no owner. Both are checked
// at build time, against the SQL the recipe would actually run.
func assertRecipeSchemas(t *testing.T, reg *recipe.Registry) {
	t.Helper()
	for _, p := range reg.Providers() {
		m, owns := p.(recipe.Migrator)
		if !owns {
			if tables := p.Declaration().Tables; len(tables) > 0 {
				t.Errorf("recipe %q declares table(s) %v but ships no migrations", p.ID(), tables)
			}
			continue
		}
		if err := recipe.ValidateSchema(p.ID(), p.Declaration(), m.Migrations()); err != nil {
			t.Error(err)
		}
	}
}

// migratingProvider is a declaration plus the DDL behind it: the smallest
// shape that exercises the schema half without a real unit claiming a table.
type migratingProvider struct {
	id     string
	tables []string
	sql    fstest.MapFS
}

func (m migratingProvider) ID() string { return m.id }

func (m migratingProvider) Declaration() recipe.Declaration {
	return recipe.Declaration{Tables: m.tables}
}

func (m migratingProvider) Routes() chi.Router { return nil }

func (m migratingProvider) Migrations() fs.FS { return m.sql }

// TestRecipeMigrationsLandThroughTheRoot proves the production path, not the
// engine: the same function the composition root calls applies the set, the
// ledger records it under the recipe's namespace, and a second boot is a no-op.
func TestRecipeMigrationsLandThroughTheRoot(t *testing.T) {
	sqlDB := openSchemaTestDB(t)
	reg := &recipe.Registry{}
	reg.Add(migratingProvider{
		id:     "probe",
		tables: []string{"ext_probe_cards"},
		sql: fstest.MapFS{
			"0001_init.sql": &fstest.MapFile{
				Data: []byte("CREATE TABLE ext_probe_cards (id TEXT PRIMARY KEY);"),
			},
		},
	})

	if err := applyRecipeMigrations(sqlDB, reg); err != nil {
		t.Fatalf("apply recipe migrations: %v", err)
	}
	if !tableInSchema(t, sqlDB, "ext_probe_cards") {
		t.Fatalf("the recipe's table did not reach the schema")
	}
	if err := applyRecipeMigrations(sqlDB, reg); err != nil {
		t.Fatalf("second boot must be a no-op, got %v", err)
	}
}

func TestRecipeMigrationsDropAdoptedTodoSeed(t *testing.T) {
	sqlDB := openSchemaTestDB(t)
	owner, legacyID, now := dbschema.NewID(), dbschema.NewID(), dbschema.Now()
	if _, err := sqlDB.ExecContext(context.Background(),
		`INSERT INTO users (id, email, password_hash, name, created, updated)
		 VALUES (?, ?, ?, ?, ?, ?)`, owner, "legacy@example.com", "hash", "Legacy", now, now); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := sqlDB.ExecContext(context.Background(),
		`INSERT INTO todos (id, owner, title, completed, idem_key, visibility, created, updated)
		 VALUES (?, ?, ?, 0, '', 'public', ?, ?)`, legacyID, owner, "legacy row", now, now); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	reg := &recipe.Registry{}
	reg.Add(todoAdoptionProvider{})
	if err := applyRecipeMigrations(sqlDB, reg); err != nil {
		t.Fatalf("apply recipe migrations: %v", err)
	}
	if !tableInSchema(t, sqlDB, "ext_todo_todos") {
		t.Fatal("the owned table did not land")
	}
	var title string
	if err := sqlDB.QueryRowContext(context.Background(),
		`SELECT title FROM ext_todo_todos WHERE id = ?`, legacyID).Scan(&title); err != nil {
		t.Fatalf("legacy row did not cross into the owned table: %v", err)
	}
	if title != "legacy row" {
		t.Fatalf("carried row changed in transit: %q", title)
	}
	if tableInSchema(t, sqlDB, "todos") {
		t.Fatal("the legacy seed must drop once the todo set has applied")
	}
	if err := applyRecipeMigrations(sqlDB, reg); err != nil {
		t.Fatalf("second apply after drop must stay a no-op: %v", err)
	}
}

type todoAdoptionProvider struct{}

func (todoAdoptionProvider) ID() string { return "todo" }

func (todoAdoptionProvider) Declaration() recipe.Declaration {
	return recipe.Declaration{Tables: []string{"ext_todo_todos"}}
}

func (todoAdoptionProvider) Routes() chi.Router { return nil }

func (todoAdoptionProvider) Migrations() fs.FS {
	return fstest.MapFS{
		"0001_init.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE ext_todo_todos (
    id         TEXT PRIMARY KEY,
    owner      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title      TEXT NOT NULL,
    completed  INTEGER NOT NULL DEFAULT 0,
    idem_key   TEXT NOT NULL DEFAULT '',
    created    TEXT NOT NULL,
    updated    TEXT NOT NULL,
    visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'public'))
);
CREATE INDEX ext_todo_todos_owner_created ON ext_todo_todos (owner, created DESC);
CREATE UNIQUE INDEX ext_todo_todos_idem_key_unique ON ext_todo_todos (owner, idem_key) WHERE idem_key <> '';
CREATE INDEX ext_todo_todos_visibility ON ext_todo_todos (visibility);
INSERT INTO ext_todo_todos (id, owner, title, completed, idem_key, created, updated, visibility)
SELECT id, owner, title, completed, idem_key, created, updated, visibility FROM todos;`)},
	}
}

// TestRecipeMigrationsWithoutAStoreFailLoud keeps the wiring honest: a set the
// root cannot apply is an error, never a silent skip.
func TestRecipeMigrationsWithoutAStoreFailLoud(t *testing.T) {
	reg := &recipe.Registry{}
	reg.Add(migratingProvider{
		id:     "probe",
		tables: []string{"ext_probe_cards"},
		sql:    fstest.MapFS{"0001_init.sql": &fstest.MapFile{Data: []byte("CREATE TABLE ext_probe_cards (id TEXT);")}},
	})
	if err := applyRecipeMigrations(nil, reg); err == nil {
		t.Fatalf("a declared set with no store must fail, not be skipped")
	}
}

// TestSchemaGateRefusesAUndeclaredCreateTable is the direction the docs said had
// no gate: a recipe owns a table and does not declare it.
func TestSchemaGateRefusesAUndeclaredCreateTable(t *testing.T) {
	reg := &recipe.Registry{}
	reg.Add(migratingProvider{
		id:     "probe",
		tables: nil,
		sql:    fstest.MapFS{"0001_init.sql": &fstest.MapFile{Data: []byte("CREATE TABLE ext_probe_cards (id TEXT);")}},
	})
	if problems := schemaProblems(reg); len(problems) == 0 {
		t.Fatalf("an undeclared CREATE TABLE must be refused")
	}
}

// TestSchemaGateAcceptsAMatchingSet closes the vacuous half: the gate has to
// stay green on a recipe whose DDL and declaration agree.
func TestSchemaGateAcceptsAMatchingSet(t *testing.T) {
	reg := &recipe.Registry{}
	reg.Add(migratingProvider{
		id:     "probe",
		tables: []string{"ext_probe_cards"},
		sql:    fstest.MapFS{"0001_init.sql": &fstest.MapFile{Data: []byte("CREATE TABLE ext_probe_cards (id TEXT);")}},
	})
	if problems := schemaProblems(reg); len(problems) != 0 {
		t.Fatalf("matching set must pass, got %v", problems)
	}
}

func schemaProblems(reg *recipe.Registry) []string {
	var out []string
	for _, p := range reg.Providers() {
		m, owns := p.(recipe.Migrator)
		if !owns {
			continue
		}
		if err := recipe.ValidateSchema(p.ID(), p.Declaration(), m.Migrations()); err != nil {
			out = append(out, err.Error())
		}
	}
	return out
}

func openSchemaTestDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/schema.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("migrate: %v", merr)
	}
	return sqlDB
}

func tableInSchema(t *testing.T, sqlDB *sql.DB, table string) bool {
	t.Helper()
	var name string
	row := sqlDB.QueryRowContext(context.Background(),
		`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`, table)
	return row.Scan(&name) == nil
}
