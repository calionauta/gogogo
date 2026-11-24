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
	"path/filepath"
	"slices"
	"strings"

	"github.com/calionauta/gogogo/internal/projectlocales"
)

// defaultLocale is the floor when neither the record nor the env names a
// language. It is a full tag because catalog files are named after it.
const defaultLocale = "en-US"

// I18NConfig selects the localization plugin surface.
type I18NConfig struct {
	// Enabled is I18N_ENABLED (default true). True resolves the request locale
	// and renders the translated strings the catalogs carry; false keeps the
	// binary on English markup (the same templates, English keys), which is the
	// opt-out for a tree that deleted the servant.
	Enabled bool
	// Supported is SUPPORTED_LOCALES (default: the tracked locales.yml record,
	// then the template's own set):
	// the tags the catalog loader must find. Invalid entries
	// warn and are skipped.
	Supported []string
	// Default is DEFAULT_LOCALE (default "en-US"), the fallback
	// when no preference matches. Must be in Supported.
	Default string
	// RequireRecipeCoverage is I18N_REQUIRE_RECIPE_COVERAGE (default false).
	// When true, a recipe that ships translations but misses a Supported tag
	// fails the boot instead of only warning. Off by default because a
	// partial catalog still renders (English fallback); the operator opts in
	// when every installed recipe must be fully translated.
	RequireRecipeCoverage bool
}

// localesFromRecord resolves the tracked language record.
//
// The record lives at the app root, and "the app root" has two honest readings:
// the working directory (how `make dev` and the container run) and the directory
// that holds DATA_DIR (how a deployment is laid out — `data/` sits inside the
// tree). Reading only "." meant a binary started from anywhere else silently
// served the template default instead of the product's declared set.
func (c *Config) localesFromRecord() []string {
	if tags, source := projectlocales.Resolve(".", ""); source == projectlocales.File {
		return tags
	}
	if c.DataDir != "" {
		if tags, source := projectlocales.Resolve(filepath.Dir(c.DataDir), ""); source == projectlocales.File {
			return tags
		}
	}
	tags, _ := projectlocales.Resolve("", "")
	return tags
}

// loadI18NEnv fills I18n from env.
func (c *Config) loadI18NEnv() {
	enabled := envBool("I18N_ENABLED", true)
	// The tracked record answers "what does this product ship"; the env var
	// answers "what does THIS deployment ship". Env wins, because a deploy must
	// override without editing a tracked file. A tree with neither gets the
	// template's own set.
	supported := parseI18NSupported(getEnv("SUPPORTED_LOCALES", ""))
	if strings.TrimSpace(getEnv("SUPPORTED_LOCALES", "")) == "" {
		supported = c.localesFromRecord()
	}
	// The record's FIRST tag is the product's default language: a second
	// hardcoded literal here would be a second answer to the same question, and
	// it drifted the moment the record said en-US and this said en.
	def := strings.TrimSpace(getEnv("DEFAULT_LOCALE", ""))
	if def == "" && len(supported) > 0 {
		def = supported[0]
	}
	if def == "" {
		def = defaultLocale
	}
	if !contains(supported, def) {
		slog.Warn("config: DEFAULT_LOCALE must be in SUPPORTED_LOCALES, using "+defaultLocale,
			"default", def, "supported", strings.Join(supported, ","))
		def = defaultLocale
		if !contains(supported, def) {
			supported = []string{defaultLocale}
		}
	}
	c.I18n.Enabled = enabled
	c.I18n.Supported = supported
	c.I18n.Default = def
	c.I18n.RequireRecipeCoverage = envBool("I18N_REQUIRE_RECIPE_COVERAGE", false)
}

// parseI18NSupported trims the CSV and drops empties.
// Unlike the search loader it keeps the raw tag shape;
// tag normalization (language.Parse) is the plugin's job
// so config never imports it.
func parseI18NSupported(raw string) []string {
	out := splitCSV(raw)
	if len(out) == 0 {
		return []string{defaultLocale}
	}
	return out
}

func contains(list []string, v string) bool {
	return slices.Contains(list, v)
}
