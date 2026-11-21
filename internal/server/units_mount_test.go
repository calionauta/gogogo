// The generated self-mounting unit list actually mounts.
//
// Two gates in internal/capabilities prove the registry, the generated file and
// the package signature agree. Neither proves the thing that matters at boot:
// that running the generated list puts a provider in the seam. This does, with
// no mux and no HTTP, so it fails for the right reason.
package server

import (
	"slices"
	"testing"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/capabilities"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/llm"
	"github.com/calionauta/gogogo/internal/queue"
	"github.com/calionauta/gogogo/internal/recipe"
	"github.com/calionauta/gogogo/internal/recipe/register"
)

// TestBundledUnitsMountThroughTheSeam drives the generated registration the way
// bootRecipes does and requires each entry to arrive as a provider with routes.
// mountBundledProviders boots the generated unit list against a temp database
// and returns the mounted providers. Every test that needs the real provider
// set uses this instead of rebuilding the deps, so a unit added to the
// generated list is exercised by all of them at once.
func mountBundledProviders(t *testing.T) []recipe.Provider {
	t.Helper()
	cfg := config.Load()
	sqlDB, err := db.OpenAt(t.TempDir() + "/units.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("db migrate: %v", merr)
	}
	q := dbgen.New(sqlDB)
	a, aerr := auth.New(q, false)
	if aerr != nil {
		t.Fatalf("auth new: %v", aerr)
	}

	// room Register starts a GoAkt host; with GoAkt disabled (the test
	// default) it registers nothing, same as production. Enable it so the
	// bundled list mounts room too — a unit that mounts itself must mount
	// when its engine is on.
	if !cfg.GoAkt.Enabled {
		cfg.GoAkt.Enabled = true
	}
	chiQ, qerr := queue.NewAt(t.TempDir() + "/chi")
	if qerr != nil {
		t.Fatalf("queue: %v", qerr)
	}
	t.Cleanup(chiQ.Close)
	reg := &recipe.Registry{}
	registerBundledUnits(reg, register.Deps{
		Cfg: cfg, Auth: a, Queries: q, SQLDB: sqlDB,
		Queue: chiQ, RealLLM: llm.New(""), SimulatedLLM: llm.NewSimulated(),
		Hub: chiQ.Hub(),
	})
	if conflicts := reg.Validate(); len(conflicts) > 0 {
		t.Fatalf("the bundled unit set is invalid: %v", conflicts)
	}
	return reg.Providers()
}

func TestBundledUnitsMountThroughTheSeam(t *testing.T) {
	providers := mountBundledProviders(t)
	if len(providers) == 0 {
		t.Fatal("the generated list mounted nothing: a unit that sets Capability.Register is unmounted at boot")
	}
	// Every entry the generated list carries must arrive as a provider with
	// routes. The expectation is the list itself, not a copy of it: a second
	// hardcoded set drifts the moment a slice is added or dropped, and it made
	// a product that dropped the demos fail a test about the seam. What is
	// asserted is the RUNTIME effect — calling the generated list puts each
	// entry in the seam — which is exactly what no static gate can prove.
	got := map[string]recipe.Provider{}
	for _, p := range providers {
		got[p.ID()] = p
	}
	if len(bundledUnits) == 0 {
		t.Fatal("the generated list is empty: every unit was pruned, so the wiring is untested")
	}
	for _, entry := range bundledUnits {
		id := entry.ID
		p, ok := got[id]
		if !ok {
			t.Fatalf("%s did not mount: providers = %v", id, providerIDs(providers))
		}
		if len(p.Declaration().Routes) == 0 {
			t.Fatalf("%s mounted with no declared routes: the provider is not the page's", id)
		}
		if p.Routes() == nil {
			t.Fatalf("%s mounted with a nil router: MountFeature would silently skip it", id)
		}
	}
}

// providerIDs lists the mounted ids for a readable failure.
func providerIDs(providers []recipe.Provider) []string {
	ids := make([]string, 0, len(providers))
	for _, p := range providers {
		ids = append(ids, p.ID())
	}
	return ids
}

// TestDeclaredActionsMatchTheRegistry closes the gap discovery leaves open.
//
// A unit spells its verbs twice: the Provider's Declaration (what the server
// actually serves through /api/ext/invoke) and the capability table in
// internal/capabilities (what `recipe describe`, the MCP and
// GET /api/ext/descriptors report). Nothing compared them, so the two could
// drift and the failure is silent and one-sided: discovery advertises a verb
// the dispatcher 404s, or the server serves one no agent can find.
//
// The comparison runs against the MOUNTED providers, not the receipt text,
// because the Declaration is the runtime truth.
func TestDeclaredActionsMatchTheRegistry(t *testing.T) {
	providers := mountBundledProviders(t)
	byCap := capabilities.ByID()

	for _, p := range providers {
		id := p.ID()
		cap, ok := byCap[id]
		if !ok {
			continue
		}
		runtime := make([]string, 0, len(p.Declaration().Actions))
		for _, a := range p.Declaration().Actions {
			runtime = append(runtime, a.Verb)
		}
		slices.Sort(runtime)
		registry := slices.Clone(cap.Actions)
		slices.Sort(registry)

		if !slices.Equal(runtime, registry) {
			t.Errorf("%s: the server serves %v but discovery advertises %v — "+
				"an agent would call a verb that 404s, or never learn one that works",
				id, runtime, registry)
		}
	}
}
