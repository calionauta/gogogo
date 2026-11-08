// Localization seam: embedded JSON catalogs plus resolution.
//
// Phase one owns lookup, plural for two locales, and
// formatting. CLDR depth is deliberately not rented yet:
// the revisit trigger is a fifth locale or the first
// language with irregular plural rules. To remove: delete
// this package, its registry entry, and the I18n config;
// markup keeps English fallbacks.
package i18n

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sync"
)

// Catalog is the immutable set of localized messages.
// It is built once at boot and never mutated; every lookup
// reads only, so handlers may share it safely.
type Catalog struct {
	messages  map[string]map[string]string
	fallback  string
	supported []string
}

// LoadFS builds a Catalog from one JSON file per tag in fsys
// (one file per supported tag, named "<tag>.json"). Every
// tag must parse, the fallback must be in supported, and its
// file must exist.
func LoadFS(fsys fs.FS, supported []string, fallback string) (*Catalog, error) {
	if len(supported) == 0 {
		return nil, errors.New("i18n: no supported locales")
	}
	if fallback == "" {
		return nil, errors.New("i18n: empty fallback locale")
	}
	c := &Catalog{messages: make(map[string]map[string]string)}
	c.supported = append(c.supported, supported...)
	c.fallback = fallback
	for _, tag := range supported {
		m, err := loadLocale(fsys, tag)
		if err != nil {
			return nil, err
		}
		if _, dup := c.messages[tag]; dup {
			return nil, fmt.Errorf("i18n: duplicate locale %q", tag)
		}
		c.messages[tag] = m
	}
	if _, ok := c.messages[fallback]; !ok {
		return nil, fmt.Errorf("i18n: fallback locale %q has no catalog", fallback)
	}
	return c, nil
}

func loadLocale(fsys fs.FS, tag string) (map[string]string, error) {
	raw, err := fs.ReadFile(fsys, "locale/"+tag+".json")
	if err != nil {
		return nil, fmt.Errorf("i18n: read catalog for %q: %w", tag, err)
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("i18n: decode catalog for %q: %w", tag, err)
	}
	return m, nil
}

// Fallback returns the locale used when a lookup misses.
func (c *Catalog) Fallback() string { return c.fallback }

// Supported returns a copy of the supported tags.
func (c *Catalog) Supported() []string {
	out := make([]string, len(c.supported))
	copy(out, c.supported)
	return out
}

// Lookup resolves key for tag, falling back to the fallback
// locale. The second result is false only when the key is
// unknown to both.
func (c *Catalog) Lookup(tag, key string) (string, bool) {
	if m, ok := c.messages[tag]; ok {
		if s, ok := m[key]; ok {
			return s, true
		}
	}
	if s, ok := c.messages[c.fallback][key]; ok {
		return s, true
	}
	return "", false
}

// MustLoadFS is LoadFS that panics on error; convenient for
// tests and one-shot wiring.
func MustLoadFS(fsys fs.FS, supported []string, fallback string) *Catalog {
	c, err := LoadFS(fsys, supported, fallback)
	if err != nil {
		panic(err)
	}
	return c
}

// bundledFS is the production embedded catalog filesystem.
// It is the single source: servers load from it, templates
// fall back to it through CatalogFromContext when no
// request catalog was injected.

//go:embed locale/en.json locale/pt-BR.json
var bundledFS embed.FS

// LoadBundled loads the shipped catalogs for the supported
// tags. A supported tag with no shipped file is a config
// error, surfaced at boot rather than a silent fallback.
func LoadBundled(supported []string, fallback string) (*Catalog, error) {
	return LoadFS(bundledFS, supported, fallback)
}

// Default returns the production catalog, loading both
// shipped locales once. It is immutable; callers share it.
func Default() *Catalog {
	return defaultOnce()
}

var defaultOnce = sync.OnceValue(func() *Catalog {
	return MustLoadFS(bundledFS, []string{"en", "pt-BR"}, "en")
})
