package recipestate

import (
	"context"
	"path/filepath"
	"testing"

	dbpkg "github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// TestMissingRowIsEnabled pins the rule the two callers share: absence is
// "installed and on", never off. Installing a recipe must not require a second
// write just to make it mount, and an explicit disable is always a row.
func TestMissingRowIsEnabled(t *testing.T) {
	var empty States
	if !empty.Enabled("anything") {
		t.Error("a missing row reads as disabled; installing a recipe would need a second write to mount")
	}
	states := States{"off": false, "on": true}
	if states.Enabled("off") {
		t.Error("an explicit disabled row reads as enabled")
	}
	if !states.Enabled("on") {
		t.Error("an explicit enabled row reads as disabled")
	}
}

// TestLoadReadsTheSwitch is the query half: the stored flag reaches Enabled, so
// the boot switch and the admin table cannot disagree about the same row.
func TestLoadReadsTheSwitch(t *testing.T) {
	ctx := context.Background()
	sqlDB, err := dbpkg.OpenAt(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err = dbschema.Migrate(sqlDB); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	q := dbgen.New(sqlDB)
	if err = q.SetRecipeState(ctx, dbgen.SetRecipeStateParams{
		ID: "notes", Enabled: 0, UpdatedBy: "test", UpdatedAt: dbschema.Now(),
	}); err != nil {
		t.Fatalf("SetRecipeState: %v", err)
	}

	states, err := Load(ctx, q)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := states["notes"]; !ok {
		t.Fatal("Load dropped the stored row; an operator disable would be invisible")
	}
	if states.Enabled("notes") {
		t.Error("a stored disabled row reads as enabled")
	}
	if !states.Enabled("todo") {
		t.Error("a row that was never written reads as disabled")
	}

	// No query set (the MCP, a fixture) is not an error: the caller's default
	// is the permissive one.
	none, err := Load(ctx, nil)
	if err != nil {
		t.Fatalf("Load(nil): %v", err)
	}
	if !none.Enabled("todo") {
		t.Error("Load(nil) does not default to enabled")
	}
}
