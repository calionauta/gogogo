// Runtime recipe switches (RECIPES_* env), split from config.go for the
// file-size gate. The durable switch lives in the core `recipe_state` table;
// this env list is the deploy-time fallback for a tree with no admin UI (a
// desktop build, a test, a no-database fork).
package config

import (
	"log/slog"
	"strings"
)

// RecipesConfig holds the deploy-time recipe switches.
type RecipesConfig struct {
	// Disabled is RECIPES_DISABLED: a comma-separated list of recipe ids that
	// must not mount even if their code is installed. Disabled is a runtime
	// state (the code stays in the tree); uninstall is the build-time removal.
	Disabled []string
}

// loadPluginEnv groups the optional servants' env loaders. One call keeps the
// top-level Load under the statement budget as the template grows more opt-in
// plugins.
func (c *Config) loadPluginEnv() {
	c.loadI18NEnv()
	c.loadRecipesEnv()
	c.loadShapeEnv()
	c.loadEventsEnv()
}

// loadRecipesEnv fills Recipes from env.
func (c *Config) loadRecipesEnv() {
	raw := strings.TrimSpace(getEnv("RECIPES_DISABLED", ""))
	if raw == "" {
		return
	}
	for id := range strings.SplitSeq(raw, ",") {
		if id = strings.TrimSpace(id); id != "" {
			c.Recipes.Disabled = append(c.Recipes.Disabled, id)
		}
	}
	if len(c.Recipes.Disabled) > 0 {
		slog.Info("config: recipes disabled by env", "ids", strings.Join(c.Recipes.Disabled, ","))
	}
}
