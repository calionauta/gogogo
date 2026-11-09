// The generated self-mounting unit list actually mounts.
//
// Two gates in internal/capabilities prove the registry, the generated file and
// the package signature agree. Neither proves the thing that matters at boot:
// that running the generated list puts a provider in the seam. This does, with
// no mux and no HTTP, so it fails for the right reason.
package server

import (
	"testing"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
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
func TestBundledUnitsMountThroughTheSeam(t *testing.T) {
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

	providers := reg.Providers()
	if len(providers) == 0 {
		t.Fatal("the generated list mounted nothing: a unit that sets Capability.Register is unmounted at boot")
	}
	if conflicts := reg.Validate(); len(conflicts) > 0 {
		t.Fatalf("the bundled unit set is invalid: %v", conflicts)
	}

	// Every bundled unit must arrive as a provider with routes. The list is
	// the registry's 12 self-mounting units (config-view is admin-only and
	// mounts via the same seam). A test that only counted providers would
	// pass if the list mounted the wrong one; pinning each id makes a
	// dropped Register fail for the right reason (red-proof: delete one
	// Register from units.gen.go and its id goes missing here).
	wantIDs := []string{
		"account", "chat", "config-view", "credits", "dashboard", "landing",
		"notes", "notifications", "review", "room", "todo", "whiteboard",
	}
	got := map[string]recipe.Provider{}
	for _, p := range providers {
		got[p.ID()] = p
	}
	for _, id := range wantIDs {
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
