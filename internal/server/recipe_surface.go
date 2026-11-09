// The composition root's recipe step: register, migrate, mount.
//
// It lives beside chi.go rather than inside buildChiMux because that function is
// at the file-size cap, and because the three steps are one decision: the code
// claims a surface, the schema answers the claim, and only then does the surface
// reach the tree.
package server

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/recipe"
	"github.com/calionauta/gogogo/internal/recipe/register"
	"github.com/calionauta/gogogo/internal/ui"
)

// bootRecipes is the one recipe step the composition root takes, in the order
// the seam requires: the generated registration tells the registry what the
// installed code claims, the registry's migration sets are applied, and only
// then are the routes mounted.
func bootRecipes(r *Router, deps register.Deps) error {
	// Bundled units first: a unit of this tree is not an install, and its
	// declaration should win the sort when an installed recipe claims the same
	// object (the conflict names both either way).
	registerBundledUnits(r.recipes, deps)
	registerInstalledRecipes(r.recipes, deps)
	if err := applyRecipeMigrations(deps.SQLDB, r.recipes); err != nil {
		return err
	}
	// Same failure class as a conflicting declaration: the composed set is wrong
	// and every request after this point would serve it.
	if merr := r.recipes.Mount(r); merr != nil {
		panic("server: " + merr.Error())
	}
	return nil
}

// shellNav turns the nav.sidebar slot into what the rail renders.
//
// The conversion lives here, not in internal/ui, so the shell keeps no knowledge
// of the recipe package: the rail receives labels and paths, and the rule that
// they are root-relative is enforced by the slot registry at registration time
// rather than by a renderer that could forget it.
func shellNav(slots *recipe.SlotRegistry) []ui.NavEntry {
	if slots == nil {
		return nil
	}
	entries := slots.Entries(recipe.SlotNavSidebar)
	out := make([]ui.NavEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, ui.NavEntry{Label: e.Label, Href: e.Href, Admin: e.Admin})
	}
	return out
}

// applyRecipeMigrations applies every recipe's own DDL through the core ledger.
//
// Why this is a second Migrate call: the registry does not exist before the
// composition root builds it, so a recipe's migration set cannot be known when
// BootChi migrates the core schema. The extra call is cheap in the steady state
// (applied versions are read first, so it is one SELECT per filesystem) and
// idempotence is per filesystem, because the ledger key is "<recipe>/<version>".
func applyRecipeMigrations(sqlDB *sql.DB, reg *recipe.Registry) error {
	sets := reg.MigrationSets()
	if len(sets) == 0 {
		return nil
	}
	if sqlDB == nil {
		return fmt.Errorf("recipe migrations: %d set(s) declared but the composition root was given no store", len(sets))
	}
	if err := dbschema.MigrateWith(sqlDB, sets...); err != nil {
		return fmt.Errorf("recipe migrations: %w", err)
	}
	if err := dropAdoptedTodoSeed(sqlDB, sets); err != nil {
		return fmt.Errorf("recipe migrations: %w", err)
	}
	return nil
}

// dropAdoptedTodoSeed finishes the todo ownership bridge-now that the
// migration set has landed. `todos` is the append-only historical seed, not
// the runtime table — runtime code reads `ext_todo_todos` — so dropping
// it is safe only after the todo migration set has applied and the owned
// table exists. It is idempotent: a fresh core-only schema recreates the
// empty seed on the next core migration, and this helper would remove it
// again after the set is applied.
func dropAdoptedTodoSeed(sqlDB *sql.DB, sets []dbschema.MigrationSet) error {
	for _, set := range sets {
		if set.ID != "todo" {
			continue
		}
		var count int
		err := sqlDB.QueryRowContext(context.Background(),
			"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'ext_todo_todos'").
			Scan(&count)
		if err != nil {
			return err
		}
		if count == 0 {
			return nil
		}
		_, err = sqlDB.ExecContext(context.Background(), "DROP TABLE IF EXISTS todos")
		return err
	}
	return nil
}
