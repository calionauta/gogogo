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

	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/recipe"
	"github.com/calionauta/gogogo/internal/recipe/register"
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
