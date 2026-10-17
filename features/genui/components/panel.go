// SCOPE:plugin — the system-panel registry: what the catalog knows, and the
// data provider a feature contributes for it.
//
// WHY A REGISTRY AND NOT A STATIC MAP. A capability declared `KindFeature` is
// exactly the thing that should be answerable from the app's own data — the
// feature owns a page, a store, and a way to list what it holds. Making each
// one a panel should cost the feature ONE file (its provider), not an edit to
// a shared map in this package plus an edit to the composition root. A static
// map made this package the hub every feature had to touch, which is the
// coupling a feature slice exists to avoid.
//
// So there is one registry per process, and the composition root installs it
// at boot from what it actually wired. Two consequences worth stating:
//
//   - What the model may emit and what can be rendered come from the SAME
//     list. They cannot drift, because a name enters the catalog and the
//     provider table together.
//   - A name with no provider is a BOOT error, not a runtime drop. The
//     previous design allowed a registered-but-unwired panel so Hydrate could
//     drop it (right for a trimmed build); that safety is preserved by the
//     boot asserting the two halves agree, which turns a silent missing panel
//     into a failed boot.
package components

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"

	"github.com/a-h/templ"
)

// PanelProvider fills one system panel's props from the app's own store. It
// lives at the consumer (this package needs rows, the store owns them, and
// nothing here learns what a todo is), and each provider names the component
// it serves, so the registry needs no switch when a panel is added.
//
// This interface is the whole contract a feature implements to become a
// native panel. Three methods and no registration call: the composition root
// discovers providers by asking the feature for one.
type PanelProvider interface {
	// Component is the catalog name this provider fills ("todo_list"). It is
	// the model-facing name, so it must be stable, snake_case, and unique.
	Component() string
	// Prompt tells the model WHEN to pick this panel. It must state the
	// request shape it answers (and, for a sliced panel, the enum), because
	// this sentence is the only place the model learns the panel exists.
	Prompt() string
	// Writable lists the props the MODEL may set — never the render shape.
	// A panel's props struct includes fields only the server may fill (a
	// list's rows), and validating against the struct would accept an answer
	// that supplies its own rows and then rely on Fill to overwrite them.
	// Declaring them here makes such an answer a rejected one (a repairable
	// shape error) instead of a provider having to remember to overwrite.
	Writable() []string
	// Fill returns the props for one answer, or an error to drop the panel.
	Fill(ctx context.Context, owner string, props map[string]any) (map[string]any, error)
	// Render turns the (already filled) props into a component. It lives with
	// the feature so a panel is ONE file: name, prompt, writable props, data
	// and rendering all belong to the feature that owns the page. The shared
	// render primitives stay in this package; the feature composes them.
	Render(props map[string]any) (templ.Component, error)
}

// panelRegistry is the process-scope catalog. Guarded by a mutex because it is
// installed once at boot and read concurrently by workers (a chat reply and an
// Ask answer can render at the same time); the read path is a map lookup, so
// the lock is only ever contended at boot.
type panelRegistry struct {
	mu        sync.RWMutex
	byName    map[string]PanelProvider
	inOrder   []string // sorted names, for stable prompts
	installed bool     // set once InstallPanelRegistry has run
}

var registry = &panelRegistry{}

// validatePanelProviders checks a provider set and returns it name-sorted.
//
// Split from InstallPanelRegistry so the invariants can be tested WITHOUT
// installing anything. That is not just tidiness: the guard tests used to
// install a bad set and rely on the install being rejected before it touched
// the registry — which was true, but meant the test's correctness depended on
// the ordering inside InstallPanelRegistry. A pure function makes the assertion
// local, and it is what lets every registry test stay read-only.
func validatePanelProviders(providers []PanelProvider) (map[string]PanelProvider, []string, error) {
	byName := make(map[string]PanelProvider, len(providers))
	names := make([]string, 0, len(providers))
	for _, p := range providers {
		name := p.Component()
		if name == "" {
			return nil, nil, fmt.Errorf("genui: panel provider %T has an empty component name", p)
		}
		if _, dup := byName[name]; dup {
			return nil, nil, fmt.Errorf("genui: two providers claim the panel %q", name)
		}
		if len(p.Writable()) == 0 {
			// A panel the model can only name, with no slice to choose, is
			// indistinguishable from prose — and an empty writable list would
			// also make validatePanelProps reject every prop, hiding the bug
			// until the first answer.
			return nil, nil, fmt.Errorf("genui: panel %q declares no writable prop "+
				"(the model must be able to choose a slice)", name)
		}
		byName[name] = p
		names = append(names, name)
	}
	sort.Strings(names)
	return byName, names, nil
}

// InstallPanelRegistry replaces the catalog with the providers the app wired.
//
// Called by the composition root at boot, before any worker starts.
//
// Re-installing with the SAME panel names is a no-op, and that is deliberate:
// it keeps `BootChi` callable more than once in one process (the composition
// tests boot the real stack a dozen times) without needing a reset escape
// hatch — a `ResetPanelRegistry` for tests is exactly the runtime-mutation seam
// this exists to close. What the guard REJECTS is a *change*: a second install
// naming a different panel set would alter what the model may emit mid-flight,
// which is never something to do silently.
//
// The providers themselves are re-bound on every call (the boot's provider
// holds that boot's *dbgen.Queries, so an old one must not linger). That is
// why a test must not swap a provider in mid-run: the re-bind is by NAME, so
// a same-name swap would silently change behavior for every other test in the
// package. Give a test fixture its own panel name instead.
func InstallPanelRegistry(providers []PanelProvider) error {
	byName, names, err := validatePanelProviders(providers)
	if err != nil {
		return err
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.installed && !equalStrings(registry.inOrder, names) {
		return fmt.Errorf("genui: the panel registry is already installed with %v; "+
			"refusing to change the model's vocabulary to %v at runtime",
			registry.inOrder, names)
	}
	registry.byName = byName
	registry.inOrder = names
	registry.installed = true
	return nil
}

// equalStrings compares two sorted name lists.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// IsPanel reports whether a catalog name is an installed system panel.
func IsPanel(name string) bool {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	_, ok := registry.byName[name]
	return ok
}

// panelNames is the registry's names, sorted, so the prompt and the guidance
// sentence are stable across runs (map order is not).
func panelNames() []string {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	return append([]string(nil), registry.inOrder...)
}

// validatePanelProps is the parse-time gate for a system panel: the model
// picks the SLICE and nothing else. An answer naming a server-owned field
// (a list's rows) is rejected here, so it is repaired like any other shape
// error instead of being quietly overwritten at hydration.
func validatePanelProps(name string, props map[string]any) error {
	registry.mu.RLock()
	p, ok := registry.byName[name]
	registry.mu.RUnlock()
	if !ok {
		return nil
	}
	writable := p.Writable()
	for key := range props {
		if !slices.Contains(writable, key) {
			return fmt.Errorf("genui: %s prop %q is not model-writable (only %v)",
				name, key, writable)
		}
	}
	return nil
}

// panelProvider returns the installed provider for a name, or nil.
func panelProvider(name string) PanelProvider {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	return registry.byName[name]
}

// renderPanel renders an installed panel through its own provider, which is
// what keeps the renderer table free of per-feature entries.
func renderPanel(name string, props map[string]any) (templ.Component, bool, error) {
	registry.mu.RLock()
	p, ok := registry.byName[name]
	registry.mu.RUnlock()
	if !ok {
		return nil, false, nil
	}
	if props == nil {
		props = map[string]any{}
	}
	comp, err := p.Render(props)
	return comp, true, err
}

// panelPrompt returns one panel's guidance line, or "" when not installed.
func panelPrompt(name string) string {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	if p, ok := registry.byName[name]; ok {
		return p.Prompt()
	}
	return ""
}
