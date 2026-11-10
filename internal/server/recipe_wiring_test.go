package server

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/capabilities"
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
		// The action table rides the same gate: every verb must resolve to
		// a route this provider declares (ActionProblems) and that route
		// must be the mounted one (the mounted set above). Either failure
		// means an agent could call a verb the tree cannot honour.
		for _, problem := range recipe.ActionProblems(p.Declaration()) {
			t.Errorf("recipe %q: %s", owner, problem)
		}
		for _, a := range p.Declaration().Actions {
			if !mounted[routeKey{method: a.Route.Method, pattern: a.Route.Pattern}] {
				t.Errorf("recipe %q action %q targets %s %s, which it does not mount",
					owner, a.Verb, a.Route.Method, a.Route.Pattern)
			}
		}
		// The registry's Actions mirror these verbs by name so
		// checkout-level discovery can advertise them without linking
		// feature handlers. The composed provider is the authority; the
		// row must match it, or discovery lies about what is invocable.
		for _, problem := range capabilityMirrorProblems(owner, p.Declaration()) {
			t.Errorf("recipe %q: %s", owner, problem)
		}
	}
}

// capabilityMirrorProblems checks the compiled-in capabilities row against
// the composed provider. An installed recipe has no row by construction —
// its discovery surface is the receipt — so absence is not drift. Only a
// bundled unit (a row exists) must mirror its Declaration.
//
// This is the fix for a gate that used to fail `go test ./internal/server`
// in every user tree that installed a recipe: the row was demanded even
// though `capabilities.All` is compiled in and no installer can write it.
func capabilityMirrorProblems(owner string, decl recipe.Declaration) []string {
	row, found := capabilities.ByID()[owner]
	if !found {
		return nil
	}
	var out []string
	wantVerbs := []string{}
	for _, a := range decl.Actions {
		wantVerbs = append(wantVerbs, a.Verb)
	}
	slices.Sort(wantVerbs)
	if len(row.Actions) != 0 || len(wantVerbs) != 0 {
		got := slices.Clone(row.Actions)
		slices.Sort(got)
		if !slices.Equal(got, wantVerbs) {
			out = append(out, fmt.Sprintf(
				"registry actions = %v, want %v (mirror the Declaration)", row.Actions, wantVerbs))
		}
	}
	// Same two-way check on the closed param sets: an agent must read
	// the allowed fields without opening the handler.
	wantParams := map[string][]string{}
	wantPathParams := map[string][]string{}
	for _, a := range decl.Actions {
		if len(a.Params) > 0 {
			wantParams[a.Verb] = slices.Clone(a.Params)
		}
		if len(a.PathParams) > 0 {
			wantPathParams[a.Verb] = slices.Clone(a.PathParams)
		}
	}
	out = append(out, mirrorParamProblems("param", row.ActionParams, wantParams)...)
	out = append(out, mirrorParamProblems("path param", row.ActionPathParams, wantPathParams)...)
	return out
}

// mirrorParamProblems compares one registry mirror map (body or path params)
// against the Declaration. Kept as one helper so the two maps cannot drift in
// different directions.
func mirrorParamProblems(kind string, got, want map[string][]string) []string {
	if len(got) == 0 && len(want) == 0 {
		return nil
	}
	missing, extra := diffParamKeys(got, want)
	switch {
	case missing != "":
		return []string{fmt.Sprintf(
			"registry %s set for %q is missing (mirror the Declaration)", kind, missing)}
	case extra != "":
		return []string{fmt.Sprintf(
			"registry advertises %ss for %q the Declaration does not declare", kind, extra)}
	}
	return nil
}

// diffParamKeys compares a registry mirror against a declaration's
// param sets: it returns the first verb whose set differs, or "".
func diffParamKeys(got, want map[string][]string) (string, string) {
	for verb, wparams := range want {
		gparams, ok := got[verb]
		if !ok {
			return verb, "missing"
		}
		wp := slices.Clone(wparams)
		gp := slices.Clone(gparams)
		slices.Sort(wp)
		slices.Sort(gp)
		if !slices.Equal(wp, gp) {
			return verb, "mismatch"
		}
	}
	for verb := range got {
		if _, ok := want[verb]; !ok {
			return verb, "extra"
		}
	}
	return "", ""
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

// TestCapabilityMirrorSkipsInstalledRecipe pins the user-tree case: a recipe
// installed from a receipt has no compiled-in capabilities row, and that
// absence must not fail the composed gate.
func TestCapabilityMirrorSkipsInstalledRecipe(t *testing.T) {
	if problems := capabilityMirrorProblems("ext_installed_probe", recipe.Declaration{}); len(problems) != 0 {
		t.Fatalf("an installed recipe has no row by construction; got %v", problems)
	}
}

// TestCapabilityMirrorStillChecksBundledUnit closes the vacuous half: the
// skip must be for absent rows only, not a blanket disable of the mirror.
func TestCapabilityMirrorStillChecksBundledUnit(t *testing.T) {
	problems := capabilityMirrorProblems("todo", recipe.Declaration{})
	if len(problems) == 0 || !strings.Contains(problems[0], "registry actions") {
		t.Fatalf("a bundled row must still be compared, got %v", problems)
	}
}

// TestCapabilityMirrorChecksPathParams closes the discovery-drift half for
// `{id}`: the registry row must advertise the path param the Declaration
// requires, or a checkout-level `recipe list` shows a verb without the id it
// needs.
func TestCapabilityMirrorChecksPathParams(t *testing.T) {
	decl := recipe.Declaration{Actions: []recipe.InvokeAction{
		{Verb: "create", Params: []string{"title", "idem_key", "visibility"}},
		{Verb: "toggle", Route: recipe.Route{Method: "POST", Pattern: "/api/todos/{id}/toggle"}},
		{Verb: "delete", Route: recipe.Route{Method: "POST", Pattern: "/api/todos/{id}/delete"}},
		{Verb: "clear_completed"},
	}}
	problems := capabilityMirrorProblems("todo", decl)
	if !strings.Contains(strings.Join(problems, "\n"), "path param") {
		t.Fatalf("missing path param mirror must be reported, got %v", problems)
	}
}

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
