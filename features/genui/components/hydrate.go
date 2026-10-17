// SCOPE:plugin — hydration seam for the app's own panel components.
package components

import (
	"context"
	"maps"
)

// Hydrate fills every installed panel's props from its provider.
//
// It reads the process registry rather than taking a provider list: the
// registry is what the prompt advertises, and a second, per-handler list was a
// second source of truth that could disagree with it (a handler passing a
// stale slice would hydrate a vocabulary the model was never told about, or
// silently drop one it was). One catalog, one lookup.
//
// A panel whose provider errors is DROPPED, and the first error is returned for
// the log — the answer keeps its other components either way, because losing a
// panel must not lose the answer. (A panel with NO provider cannot reach here:
// the boot asserts every advertised name has one.)
//
// It returns a NEW slice with fresh prop maps. Writing through the caller's
// map (maps are references) would make the returned slice redundant and
// hide the aliasing from any test written against the return value.
func Hydrate(
	ctx context.Context, dirs []Directive, owner string,
) ([]Directive, error) {
	var firstErr error
	kept := make([]Directive, 0, len(dirs))
	for _, d := range dirs {
		if !IsPanel(d.Component) {
			// Not a panel: passes through untouched.
			kept = append(kept, d)
			continue
		}
		provider := panelProvider(d.Component)
		if provider == nil {
			// Unreachable after a valid boot; dropping rather than rendering
			// empty keeps the failure honest if that ever stops being true.
			continue
		}
		props, err := provider.Fill(ctx, owner, d.Props)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		filled := make(map[string]any, len(props))
		maps.Copy(filled, props)
		d.Props = filled
		kept = append(kept, d)
	}
	return kept, firstErr
}

// PanelNamesIn reports which of dirs are system panels, so a caller can log
// or count them without knowing their names.
func PanelNamesIn(dirs []Directive) []string {
	var out []string
	for _, d := range dirs {
		if IsPanel(d.Component) {
			out = append(out, d.Component)
		}
	}
	return out
}
