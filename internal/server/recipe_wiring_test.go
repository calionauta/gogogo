package server

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/db"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/recipe"
)

// assertDeclarationsMatchMounts is the phase-2 gate: a recipe's DECLARATION and
// the routes it actually mounts must be the same set, in both directions.
//
// One direction catches an undeclared route (it would reach the tree with no
// policy row and no nav story); the other catches a declared route nothing
// serves. The claim table already records who mounted what, so this needs no
// second inventory.
func assertDeclarationsMatchMounts(t *testing.T, mux *Router, reg *recipe.Registry) {
	t.Helper()
	for _, p := range reg.Providers() {
		owner := p.ID()
		declared := map[routeKey]bool{}
		for _, rt := range p.Declaration().Routes {
			declared[routeKey{method: rt.Method, pattern: rt.Pattern}] = true
		}
		mounted := map[routeKey]bool{}
		for key, who := range mux.routes.owner {
			if who == owner {
				mounted[key] = true
			}
		}
		for key := range declared {
			if !mounted[key] {
				t.Errorf("recipe %q declares %s %s, which it does not mount", owner, key.method, key.pattern)
			}
		}
		for key := range mounted {
			if !declared[key] {
				t.Errorf("recipe %q mounts %s %s without declaring it", owner, key.method, key.pattern)
			}
		}
	}
}

// assertRecipeWiring runs both phase-2 gates over the real composed tree: the
// declaration equals what mounted, and every declared table exists.
func assertRecipeWiring(t *testing.T, mux *Router, sqlDB *sql.DB) {
	t.Helper()
	assertDeclarationsMatchMounts(t, mux, mux.recipes)
	assertRecipeSchemas(t, mux.recipes)
	assertDeclaredTablesExist(t, sqlDB, mux.recipes)
}

// declaredTableProblems is the schema half: a recipe that declares a table must
// find it in the schema. In phase 2 the table still arrives through the core
// migration, so the check proves the declaration TRUE rather than proving the
// recipe created it.
func declaredTableProblems(sqlDB *sql.DB, reg *recipe.Registry) []string {
	var out []string
	for _, p := range reg.Providers() {
		for _, table := range p.Declaration().Tables {
			var name string
			row := sqlDB.QueryRowContext(context.Background(),
				`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`, table)
			if err := row.Scan(&name); err != nil {
				out = append(out, p.ID()+": declares table "+table+", which is not in the schema")
			}
		}
	}
	return out
}

// assertDeclaredTablesExist fails on any declaration the schema does not answer.
func assertDeclaredTablesExist(t *testing.T, sqlDB *sql.DB, reg *recipe.Registry) {
	t.Helper()
	for _, problem := range declaredTableProblems(sqlDB, reg) {
		t.Error(problem)
	}
}

// fakeProvider is a declaration with no code behind it: enough to prove the
// table gate reports a missing table and accepts a real one.
type fakeProvider struct {
	id     string
	tables []string
}

func (f fakeProvider) ID() string { return f.id }

func (f fakeProvider) Declaration() recipe.Declaration {
	return recipe.Declaration{Tables: f.tables}
}

func (f fakeProvider) Routes() chi.Router { return nil }

func TestDeclaredTableGateReportsAMissingTable(t *testing.T) {
	sqlDB, err := db.OpenAt(t.TempDir() + "/tables.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("migrate: %v", merr)
	}

	reg := &recipe.Registry{}
	reg.Add(fakeProvider{id: "probe", tables: []string{"todos", "ext_probe_absent"}})

	problems := declaredTableProblems(sqlDB, reg)
	if len(problems) != 1 || !strings.Contains(problems[0], "ext_probe_absent") {
		t.Fatalf("expected exactly the missing table reported, got %v", problems)
	}
}

// TestDeclaredTableGateAcceptsARealTable closes the gate's vacuous half. Until
// a unit declared a table, "no problems" could have meant "nothing checked":
// the negative proved the miss, and this proves the hit. `notes` is used
// because it exists in the ledger and no conversion has claimed it.
func TestDeclaredTableGateAcceptsARealTable(t *testing.T) {
	sqlDB, err := db.OpenAt(t.TempDir() + "/tables-ok.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("migrate: %v", merr)
	}

	reg := &recipe.Registry{}
	reg.Add(fakeProvider{id: "probe", tables: []string{"notes"}})

	if problems := declaredTableProblems(sqlDB, reg); len(problems) != 0 {
		t.Fatalf("a declared table that exists must pass, got %v", problems)
	}
}

// assertCoreLedgerApplied reads the shared ledger and demands the embedded core
// set exactly: every version once, none invented. A recipe adds its own half
// under "<id>/<version>", so the comparison is over the unprefixed rows — and
// an unprefixed row that is not embedded is still the drift this pins.
func assertCoreLedgerApplied(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	rows, err := sqlDB.QueryContext(context.Background(),
		`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatalf("read migrations: %v (seam did not migrate)", err)
	}
	defer rows.Close()
	var versions []string
	for rows.Next() {
		var v string
		if serr := rows.Scan(&v); serr != nil {
			t.Fatalf("scan: %v", serr)
		}
		versions = append(versions, v)
	}
	if rerr := rows.Err(); rerr != nil {
		t.Fatalf("rows: %v", rerr)
	}
	want, verr := dbschema.Versions()
	if verr != nil {
		t.Fatalf("expected versions: %v", verr)
	}
	core := make([]string, 0, len(versions))
	for _, v := range versions {
		if !strings.Contains(v, "/") {
			core = append(core, v)
		}
	}
	if fmt.Sprintf("%v", core) != fmt.Sprintf("%v", want) {
		t.Fatalf("applied core ledger = %v, want embedded %v (recipe sets: %v)",
			core, want, versions)
	}
}
