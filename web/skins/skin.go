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
	"net/http"
	"sync"

	"github.com/a-h/templ"
)

const defaultName = "daisyui"

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

	// Fallback to default. Registration happens in the app wiring, which is
	// not present when a single component package is rendered in a test or a
	// trimmed build; DefaultAssets keeps that path rendering instead of
	// panicking and hides no production misconfiguration (the warning below
	// still fires for an unknown name).
	mu.RLock()
	s, ok := skins[defaultName]
	mu.RUnlock()
	if !ok {
		if name != "" && name != defaultName {
			slog.Warn("skins: unknown skin, falling back to 'daisyui'", "name", name)
		}
		return Skin{Name: defaultName, Assets: DefaultAssets}
	}
	if name != "" && name != defaultName {
		slog.Warn("skins: unknown skin, falling back to 'daisyui'", "name", name)
	}
	return s
}

// DefaultAssets is the DaisyUI asset block. It lives here (not only in the
// daisyui package) so Active can render a sane default when no skin has been
// registered, which is the normal state in a single-package component test.
func DefaultAssets() templ.Component {
	return templ.Raw(`
<link rel="stylesheet" href="/static/app.min.css"/>
<link rel="stylesheet" href="/static/app.css"/>
<script src="/static/iconify-icon.min.js"></script>
<script defer type="module" src="/static/theme.js"></script>
<script defer type="module" src="/static/datastar.js"></script>
	`)
}

// ActiveAssets is a convenience wrapper for use inside Templ templates:
// it looks up the active skin and returns its Assets component.
//
//	@skins.ActiveAssets(skinName)
func ActiveAssets(name string) templ.Component {
	return Active(name).Assets()
}

// Resolve returns the skin a request should render with: the ?skin= query
// wins when it names a registered skin, then the configured default, then
// daisyui. Every page renders its assets through this, so a skin selected in
// the navbar applies to the whole app, not just the reference feature.
func Resolve(r *http.Request, fallback string) string {
	if q := r.URL.Query().Get("skin"); q != "" {
		if registered(q) {
			return q
		}
		return "daisyui"
	}
	if registered(fallback) {
		return fallback
	}
	return "daisyui"
}

func registered(name string) bool {
	mu.RLock()
	defer mu.RUnlock()
	_, ok := skins[name]
	return ok
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
