// The localization plugin's env knobs, split from config.go
// for the file-size gate. Config keeps one I18n field.
//
// The defaults live HERE, not in internal/i18n, because config
// is core and the plugin is removable: importing the plugin
// would make `gogogo trim i18n` break the build on a missing
// package. internal/i18n stays the authority for catalog
// loading; unknown locales fall back to the default rather
// than failing the boot.
package config

import (
	"log/slog"
	"slices"
	"strings"
)

// I18NConfig selects the localization plugin surface.
type I18NConfig struct {
	// Enabled is I18N_ENABLED (default true). True resolves the request locale
	// and renders the translated strings the catalogs carry; false keeps the
	// binary on English markup (the same templates, English keys), which is the
	// opt-out for a tree that deleted the servant.
	Enabled bool
	// Supported is SUPPORTED_LOCALES (default "en,pt-BR"):
	// the tags the catalog loader must find. Invalid entries
	// warn and are skipped.
	Supported []string
	// Default is DEFAULT_LOCALE (default "en"), the fallback
	// when no preference matches. Must be in Supported.
	Default string
}

// loadI18NEnv fills I18n from env.
func (c *Config) loadI18NEnv() {
	enabled := envBool("I18N_ENABLED", true)
	supported := parseI18NSupported(getEnv("SUPPORTED_LOCALES", "en,pt-BR"))
	def := strings.TrimSpace(getEnv("DEFAULT_LOCALE", "en"))
	if def == "" {
		def = "en"
	}
	if !contains(supported, def) {
		slog.Warn("config: DEFAULT_LOCALE must be in SUPPORTED_LOCALES, using en",
			"default", def, "supported", strings.Join(supported, ","))
		def = "en"
		if !contains(supported, def) {
			supported = []string{"en"}
		}
	}
	c.I18n.Enabled = enabled
	c.I18n.Supported = supported
	c.I18n.Default = def
}

// parseI18NSupported trims the CSV and drops empties.
// Unlike the search loader it keeps the raw tag shape;
// tag normalization (language.Parse) is the plugin's job
// so config never imports it.
func parseI18NSupported(raw string) []string {
	out := splitCSV(raw)
	if len(out) == 0 {
		return []string{"en"}
	}
	return out
}

func contains(list []string, v string) bool {
	return slices.Contains(list, v)
}
