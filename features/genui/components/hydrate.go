// SCOPE:plugin — hydration seam for the app's own panel components.
package components

import (
	"context"
	"maps"
)

// Hydrate fills every registered system component's props from its provider.
//
// A REGISTERED panel with no provider (trimmed build) is DROPPED, not
// rendered empty: an empty panel would tell the user "you have no todos",
// which is a lie the app must not tell when nothing was wired. A panel whose
// provider errors is dropped too, and the first error is returned for the log
// — the answer keeps its other components either way, because losing a panel
// must not lose the answer.
//
// It returns a NEW slice with fresh prop maps. Writing through the caller's
// map (maps are references) would make the returned slice redundant and
// hide the aliasing from any test written against the return value.
func Hydrate(
	ctx context.Context, dirs []Directive, owner string, providers []PanelProvider,
) ([]Directive, error) {
	byName := make(map[string]PanelProvider, len(providers))
	for _, p := range providers {
		byName[p.Component()] = p
	}
	var firstErr error
	kept := make([]Directive, 0, len(dirs))
	for _, d := range dirs {
		provider, wired := byName[d.Component]
		if !wired {
			// A registered panel without a provider is dropped; anything
			// that is not a registered panel passes through untouched.
			if !IsPanel(d.Component) {
				kept = append(kept, d)
			}
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
