// The one place the localization plugin meets the
// composition root. Split from chi.go for the function-length
// gate, mirroring bootSearch.
//
// The plugin is removable: this file plus the registry entry
// are the whole seam. With the flag off the handler is
// untouched (identical behavior); templates render the
// embedded English catalog through the same ctx helpers.
package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/i18n"
	"github.com/calionauta/gogogo/internal/recipe"
)

// bootI18N builds the request catalog once. Nil catalog
// means disabled; templates fall back to the embedded
// English catalog through CatalogFromContext.
func bootI18N(cfg *config.Config, recipes []recipe.TranslationSet) (*i18n.Catalog, error) {
	if !cfg.I18n.Enabled {
		return nil, nil
	}
	supported, err := i18n.ParseSupported(joinConfigSupported(cfg.I18n.Supported))
	if err != nil {
		return nil, fmt.Errorf("server: i18n supported %q: %w",
			joinConfigSupported(cfg.I18n.Supported), err)
	}
	cat, err := i18n.LoadBundled(supported, cfg.I18n.Default)
	if err != nil {
		return nil, fmt.Errorf("server: i18n catalog: %w", err)
	}
	if len(recipes) == 0 {
		return cat, nil
	}
	// Recipes bring their own strings and the engine prefixes them with the
	// recipe id, so a slice can be copied in or deleted without touching a core
	// catalog file. A collision (impossible with the prefix, unless a recipe id
	// repeats a core prefix) refuses the boot: a silently shadowed string is
	// worse than a failed start.
	sets := make([]i18n.RecipeCatalog, 0, len(recipes))
	for _, r := range recipes {
		sets = append(sets, i18n.RecipeCatalog{ID: r.ID, FS: r.FS})
	}
	merged, merr := cat.WithRecipes(sets)
	if merr != nil {
		return nil, fmt.Errorf("server: i18n recipe catalog: %w", merr)
	}
	// A recipe that ships only some locales is not an error (the fallback
	// answers), but it must be visible: the gap shows as an English word inside
	// a translated page and nothing else notices.
	for _, gap := range i18n.CheckRecipeCoverage(supported, sets) {
		slog.Warn("server: i18n coverage gap", "detail", gap)
	}
	return merged, nil
}

// applyI18N loads the request catalog and wraps next with
// locale resolution. Disabled is a pass-through: no context
// change, no response change. A bad catalog degrades to
// English with a warning rather than refusing the boot.
func applyI18N(cfg *config.Config, recipes []recipe.TranslationSet, next http.Handler) http.Handler {
	cat, err := bootI18N(cfg, recipes)
	if err != nil {
		slog.Warn("server: i18n catalog, using default", "error", err)
		return next
	}
	if !cfg.I18n.Enabled || cat == nil {
		return next
	}
	supported, err := i18n.ParseSupported(joinConfigSupported(cfg.I18n.Supported))
	if err != nil {
		slog.Warn("server: i18n supported, using default", "error", err)
		return next
	}
	return i18n.CatalogMiddleware(cat, supported, cfg.I18n.Default, next)
}

func joinConfigSupported(supported []string) string {
	if len(supported) == 0 {
		return "en"
	}
	return strings.Join(supported, ",")
}

// registerLocaleRoute mounts the language picker's write half.
//
// It lives here because this file owns the whole i18n seam: the catalog is
// built here, the middleware is applied here, and the route that writes the
// preference belongs beside them. A bad catalog degrades to "no picker" rather
// than failing the route — the same rule applyI18N follows at boot.
func registerLocaleRoute(r *Router, cfg *config.Config) {
	cat, err := bootI18N(cfg, nil)
	if err != nil {
		cat = nil
	}
	r.Claim("core", "GET", "/_ui/locale")
	r.Get("/_ui/locale", localeHandler(cat))
}
