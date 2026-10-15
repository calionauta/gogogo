// Package skins implements the pluggable UI skin dispatcher for the
// gogogo template. Every skin is compiled into the binary (no build
// tags); the active skin is selected at runtime by UI_SKIN (env var).
//
// A skin is a named set of assets (CSS/JS) that the layout injects.
// The template dispatch (which template renders the todo page) is
// handled by the todo handler, which reads the active skin name from
// config and calls the appropriate template function.
//
// See AGENTS.md / ui-skin-plugin-plan-v3.md for the full design.
package skins

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/a-h/templ"
)

// Skin defines one pluggable UI skin.
type Skin struct {
	Name string

	// Assets returns the <link>/<script> templ elements for this skin's
	// static assets (CSS, JS). These are injected into the <head> of
	// the rendered page by the layout dispatcher.
	Assets func() templ.Component
}

var (
	mu    sync.RWMutex
	skins = map[string]Skin{}
)

// Register adds a skin to the global registry. Panics if a skin with
// the same name is already registered (defensive — init-time bug).
// Called from package init() functions in each skin directory.
func Register(s Skin) {
	mu.Lock()
	defer mu.Unlock()
	if _, ok := skins[s.Name]; ok {
		panic(fmt.Sprintf("skins: duplicate registration for %q", s.Name))
	}
	skins[s.Name] = s
}

// Active returns the currently active skin by name. Falls back to the
// "daisyui" skin when name is empty or unknown, logging a warning.
// The default "daisyui" skin must be registered before the first call.
func Active(name string) Skin {
	mu.RLock()
	if s, ok := skins[name]; ok {
		mu.RUnlock()
		return s
	}
	mu.RUnlock()

	// Fallback to default
	mu.RLock()
	s, ok := skins["daisyui"]
	mu.RUnlock()
	if !ok {
		panic("skins: default skin 'daisyui' not registered")
	}
	if name != "" && name != "daisyui" {
		slog.Warn("skins: unknown skin, falling back to 'daisyui'", "name", name)
	}
	return s
}

// ActiveAssets is a convenience wrapper for use inside Templ templates:
// it looks up the active skin and returns its Assets component.
//
//	@skins.ActiveAssets(skinName)
func ActiveAssets(name string) templ.Component {
	return Active(name).Assets()
}

// List returns the names of all registered skins.
func List() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(skins))
	for n := range skins {
		names = append(names, n)
	}
	return names
}
