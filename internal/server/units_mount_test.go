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

	reg := &recipe.Registry{}
	registerBundledUnits(reg, register.Deps{Cfg: cfg, Auth: a, Queries: q, SQLDB: sqlDB})

	providers := reg.Providers()
	if len(providers) == 0 {
		t.Fatal("the generated list mounted nothing: a unit that sets Capability.Register is unmounted at boot")
	}
	if conflicts := reg.Validate(); len(conflicts) > 0 {
		t.Fatalf("the bundled unit set is invalid: %v", conflicts)
	}

	// account is the converted unit, and the assertion names it: a test that only
	// counted providers would pass if the list mounted the wrong one.
	var found bool
	for _, p := range providers {
		if p.ID() != "account" {
			continue
		}
		found = true
		if len(p.Declaration().Routes) == 0 {
			t.Fatalf("account mounted with no declared routes: the provider is not the page's")
		}
		if p.Routes() == nil {
			t.Fatalf("account mounted with a nil router: MountFeature would silently skip it")
		}
	}
	if !found {
		t.Fatalf("account did not mount: providers = %v", providerIDs(providers))
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
