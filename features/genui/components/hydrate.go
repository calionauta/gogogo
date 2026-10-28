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
// A panel whose provider errors is replaced by `unavailable`, not dropped.
//
// Dropping it used to be the rule, on the theory that "you have no todos"
// would be a lie — true, but a silently MISSING panel is a different lie, and
// it is the one users hit: the request was understood, the model emitted the
// right component, hydration failed for a reason it could not see, and the
// answer came back with a hole where the only useful part was. An explicit
// "could not load — open the page" line is honest about the same failure and
// gives the user somewhere to go. (Only a panel with no provider at all is
// still dropped: that is a boot bug, unreachable after a valid install.)
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
			// Unreachable after a valid boot (the boot asserts every advertised
			// name has one); dropping rather than rendering empty keeps that
			// bug loud if it ever stops being true.
			continue
		}
		// Re-validate, because this is where a shape that slipped past parse
		// (a server-owned field, a spelling the alias table does not cover)
		// would otherwise be answered with an empty panel.
		if err := validatePanelProps(d.Component, d.Props); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			kept = append(kept, unavailableDirective(d, err))
			continue
		}
		props, err := provider.Fill(ctx, owner, d.Props)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			kept = append(kept, unavailableDirective(d, err))
			continue
		}
		filled := make(map[string]any, len(props))
		maps.Copy(filled, props)
		d.Props = filled
		kept = append(kept, d)
	}
	return kept, firstErr
}

// Rehydrate refreshes STORED directives before a history render.
//
// Hydrate is the parse-time gate: it validates the MODEL's props, so a panel
// that carries rows (or any other Fill output) fails it by design ("prop rows
// is not model-writable"). A stored answer is not model output — it is Fill's
// result — so passing it back through Hydrate made every chat reload log that
// warning and fall back to the snapshot from answer time, which meant a
// mutation made after the answer never showed up on a reload. The path was
// invisible in tests that stored a model-shaped directive, because that is the
// one shape a real worker never stores.
//
// Rehydrate rebuilds the input from the model-WRITABLE props only, keeps the
// server-minted instance id (a later patch aims at it), and runs Fill again.
// A fill that fails keeps the stored directive rather than replacing it with
// the unavailable placeholder: its rows were real when stored, and a transient
// store error must not blank an answer the user can still read. The error is
// returned for the caller to log; the slice is still the best renderable state.
func Rehydrate(ctx context.Context, dirs []Directive, owner string) ([]Directive, error) {
	var firstErr error
	kept := make([]Directive, 0, len(dirs))
	for _, d := range dirs {
		if !IsPanel(d.Component) {
			kept = append(kept, d)
			continue
		}
		provider := panelProvider(d.Component)
		if provider == nil {
			// A boot bug (Hydrate drops these too), but a reload must not take
			// the rest of the answer down over it.
			kept = append(kept, d)
			continue
		}
		filled, err := provider.Fill(ctx, owner, writableProps(provider, d.Props))
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			kept = append(kept, d)
			continue
		}
		out := make(map[string]any, len(filled)+1)
		maps.Copy(out, filled)
		// Fill must not decide a slot's DOM address; keep the stored one even
		// if a provider drops it from its return value.
		if id, ok := d.Props[InstanceIDProp]; ok {
			out[InstanceIDProp] = id
		}
		refreshed := d
		refreshed.Props = out
		kept = append(kept, refreshed)
	}
	return kept, firstErr
}

// writableProps is a stored panel's input, rebuilt: the props the MODEL owns
// plus the instance id. Everything else in a stored directive is Fill's output
// (rows, counts, cursors), and re-sending it to Fill is either rejected by the
// writable gate or, worse, accepted by a provider that copies unknown keys
// through — leaking a stale value into a fresh answer.
func writableProps(p PanelProvider, props map[string]any) map[string]any {
	out := make(map[string]any, len(p.Writable())+1)
	for _, key := range p.Writable() {
		if v, ok := props[key]; ok {
			out[key] = v
		}
	}
	if id, ok := props[InstanceIDProp]; ok {
		out[InstanceIDProp] = id
	}
	return out
}

// unavailableDirective is the panel-shaped placeholder for a fill that
// failed. It keeps the component NAME (so the answer still reads as the thing
// the user asked for) and carries a short, human sentence instead of rows; the
// renderers turn it into a link to the page that owns the data. The reason is
// deliberately not shown to the user — it is logged by the caller, and
// overflow errors/models' spelling mistakes are not user-facing text.
func unavailableDirective(d Directive, err error) Directive {
	// The instance id is CARRIED OVER, not dropped. It is this slot's DOM id:
	// losing it rendered a placeholder with an empty id, so the region a user
	// can see had no address — a later successful answer could not morph it in
	// place and stacked a second panel beside it instead (measured: the
	// placeholder came back with `id=""`).
	return Directive{
		Component: d.Component,
		Props: map[string]any{
			InstanceIDProp:  d.InstanceID(),
			UnavailableProp: unavailableReason(err),
		},
		Children: d.Children,
	}
}

// unavailableReason turns a fill error into the one line the placeholder
// shows. Kept short on purpose: the panel is a slice of the page the user can
// visit, so the useful instruction is "go there", not a diagnosis.
func unavailableReason(error) string {
	return "Couldn't load this from your data just now. Open the app page to see it."
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
