// Runtime recipe switches: the one place the durable recipe_state table and
// the deploy-time RECIPES_DISABLED list become a predicate the generated
// registration can call.
//
// The switch is evaluated ONCE at boot, deliberately. A disabled recipe must
// not start its workers, consumers or background tickers — gating only the
// routes at request time would leave them running. A change from /config
// therefore takes effect on the next boot, and the admin page says so.
package server

import (
	"context"
	"log/slog"

	"github.com/calionauta/gogogo/config"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/recipestate"
)

// installedRecipeIDs is every id the generated registration knows. It is the
// build fact the admin Units table needs for a recipe that is installed but
// disabled (and therefore not mounted).
func installedRecipeIDs() []string {
	out := make([]string, 0, len(bundledUnits)+len(installedRecipes))
	for _, entry := range bundledUnits {
		out = append(out, entry.ID)
	}
	for _, entry := range installedRecipes {
		out = append(out, entry.ID)
	}
	return out
}

// recipeEnabledSwitch builds the predicate the generated registration calls.
// A missing row means enabled: installing a recipe must not require a second
// write before it mounts. The env list wins over the table so a deploy can
// always start with a capability off.
func recipeEnabledSwitch(ctx context.Context, cfg *config.Config, q *dbgen.Queries) func(string) bool {
	states, err := recipestate.Load(ctx, q)
	if err != nil {
		slog.Warn("server: recipe states unavailable, defaulting to enabled", "error", err)
	}
	disabledByEnv := map[string]bool{}
	if cfg != nil {
		for _, id := range cfg.Recipes.Disabled {
			disabledByEnv[id] = true
		}
	}
	return func(id string) bool {
		if disabledByEnv[id] {
			return false
		}
		return states.Enabled(id)
	}
}
