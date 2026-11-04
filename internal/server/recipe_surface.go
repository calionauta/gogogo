// SCOPE:core — the composition root's recipe step: register, migrate, mount.
//
// It lives beside chi.go rather than inside buildChiMux because that function is
// at the file-size cap, and because the three steps are one decision: the code
// claims a surface, the schema answers the claim, and only then does the surface
// reach the tree.
package server

import (
	"database/sql"
	"fmt"

	"github.com/calionauta/gogogo/features/notifications"
	notificationshandlers "github.com/calionauta/gogogo/features/notifications/handlers"
	"github.com/calionauta/gogogo/features/todo"
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

// bootInbox builds the inbox slice inside the composition instead of after it.
//
// The slice used to mount later because the shell's session stream shares its
// bus, which looked like a reason to build it outside the tree. It is not: the
// bus is just another dependency, so the root builds both here, keeps them for
// the middleware step, and the routes travel the same seam as every other unit.
func bootInbox(r *Router, deps register.Deps) {
	bus := notifications.NewBus()
	h := notificationshandlers.NewHandler(deps.Auth, deps.Cfg, deps.Queries, bus, buildDelivery(deps.Cfg))
	r.inbox, r.inboxBus = h, bus
	r.recipes.Add(h.Recipe())
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
		out = append(out, ui.NavEntry{Label: e.Label, Href: e.Href})
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
	return nil
}

// rowActionsFor copies the row-action slot into the todo handler's shape.
//
// It runs at the one moment the registry is known to be complete: after the
// installed recipes and the in-tree units registered, before anything serves.
func rowActionsFor(slots *recipe.SlotRegistry) []todo.RowAction {
	if slots == nil {
		return nil
	}
	actions := slots.Actions(recipe.SlotRecordRowActions)
	if len(actions) == 0 {
		return nil
	}
	out := make([]todo.RowAction, 0, len(actions))
	for _, a := range actions {
		out = append(out, todo.RowAction{Label: a.Label, Href: a.Href, Verb: a.Verb, Confirm: a.Confirm})
	}
	return out
}
