// SCOPE:plugin — rendering a parsed directive into a component, with ids,
// nesting bounds and panel substitution.
//
// Split from catalog.go for the file-size gate, and along a real seam: that file
// owns the VOCABULARY (which names exist, what props each takes, how they are
// validated); this one owns turning an already-validated directive into markup.
// The split also puts the two contracts a reviewer most needs to read side by
// side in one file — the instance id every component gets, and the placeholder a
// failed panel becomes.
package components

import (
	"errors"
	"fmt"
	"strings"

	"github.com/a-h/templ"
)

// Render resolves one validated directive to its component. Unknown
// names fail even if ParseDirectives was skipped — defense in depth,
// same fail-closed shape as RequireOwner behind every handler.
func Render(d Directive) (templ.Component, error) {
	return renderWithPrefix(d, "", new(int))
}

// RenderAll renders a directive list, stopping at the first failure
// (a half-rendered generative region is worse than an error toast).
//
// An optional id prefix makes every id in this answer unique on a page that
// already holds other answers: a chat thread keeps its history, so the second
// answer's components would otherwise repeat the first one's ids. Callers that
// replace a single region pass nothing and let each component mint its
// own; callers that append to a growing page (chat) pass a per-message stem.
func RenderAll(dirs []Directive) ([]templ.Component, error) {
	return RenderAllWithPrefix(dirs, "")
}

// RenderAllWithPrefix is RenderAll with an explicit id prefix, applied to every
// component rendered from this list (see instance.go for why ids exist).
func RenderAllWithPrefix(dirs []Directive, idPrefix string) ([]templ.Component, error) {
	comps := make([]templ.Component, 0, len(dirs))
	seq := 0
	for _, d := range dirs {
		comp, err := renderWithPrefix(d, idPrefix, &seq)
		if err != nil {
			// A panel that cannot render is replaced, not fatal. Panels are
			// the only components whose props come from the store, so theirs is
			// the one failure a user can hit through no fault of the model —
			// and aborting the whole answer over it left the tab with nothing
			// (the live "empty result" report). Non-panel failures stay fatal:
			// those are a bad answer, and a half-rendered one is worse.
			if IsPanel(d.Component) {
				comps = append(comps, PanelUnavailable(d.InstanceID(), d.Component, panelHomePage(d.Component)))
				continue
			}
			return nil, err
		}
		comps = append(comps, comp)
	}
	return comps, nil
}

// renderChildren renders a container's nested directives, keeping the loop's
// error arm out of renderDepth (one flat function beats a nested if).
func renderChildren(children []Directive, depth int, idPrefix string, seq *int) ([]templ.Component, error) {
	comps := make([]templ.Component, 0, len(children))
	for _, child := range children {
		comp, err := renderDepthAt(child, depth+1, idPrefix, seq)
		if err != nil {
			return nil, err
		}
		comps = append(comps, comp)
	}
	return comps, nil
}

// renderWithPrefix is the real renderer. idPrefix+seq build the id for a
// directive that has none; a directive that already carries one keeps it (that
// is the parse-time assignment, so the id a handler computed is the id the
// browser sees).
func renderWithPrefix(d Directive, idPrefix string, seq *int) (templ.Component, error) {
	return renderDepthAt(d, 0, idPrefix, seq)
}

// renderDepthAt is renderWithPrefix with a nesting budget: one enforcement
// point for both the parse path and direct Render callers.
func renderDepthAt(d Directive, depth int, idPrefix string, seq *int) (templ.Component, error) {
	if depth > maxDirectiveDepth {
		return nil, fmt.Errorf("genui: directives nested deeper than %d", maxDirectiveDepth)
	}
	if d.Component == sectionName {
		// decodeProps, not a hand-rolled decoder: it is the same routine every
		// leaf uses, so the container cannot accept props a leaf would reject
		// (this inline copy was the one place that could drift).
		title, err := decodeProps[SectionProps](d.Props)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(title.Title) == "" {
			return nil, errors.New("genui: section requires title")
		}
		children, err := renderChildren(d.Children, depth, idPrefix, seq)
		if err != nil {
			return nil, err
		}
		return GenuiSection(instanceIDFor(&d, idPrefix, seq), title.Title, children), nil
	}
	// Installed panels render through their own provider, so no per-feature
	// entry lives in the renderer table. Checked before the generative
	// registry because a name can only be one or the other (the boot asserts
	// that a panel name is not also a leaf).
	//
	// The id rides in the props, so a provider renders its own root id and two
	// instances of the same panel cannot collide on a page holding a history.
	props := d.Props
	if props == nil {
		props = map[string]any{}
	}
	props[InstanceIDProp] = instanceIDFor(&d, idPrefix, seq)
	// A panel replaced by Hydrate because its data could not be read carries
	// `unavailable`: render the PLACEHOLDER, never the provider.
	//
	// Without this short-circuit the placeholder was a dead letter — the
	// provider ran anyway and painted its own empty panel (measured: a failing
	// panel rendered "stub-panel" with no rows and the reason was never shown).
	// The check is here, before renderPanel, because only the renderer knows it
	// is looking at a substitute rather than a real fill.
	if _, ok := props[UnavailableProp].(string); ok && IsPanel(d.Component) {
		// The reason is deliberately NOT rendered: it carries either an
		// internal overflow detail or the model's spelling, neither of which is
		// user-facing copy. The placeholder explains the situation and points at
		// the page the PROVIDER says owns the data.
		id, _ := props[InstanceIDProp].(string)
		return PanelUnavailable(id, d.Component, panelHomePage(d.Component)), nil
	}
	if comp, isPanel, err := renderPanel(d.Component, props); isPanel {
		return comp, err
	}
	r, ok := renderers[d.Component]
	if !ok {
		return nil, fmt.Errorf("genui: unregistered component %q", d.Component)
	}
	return r(props)
}

// instanceIDFor resolves the id a directive should render with: the one it
// already carries, else `prefix-N` when a prefix was supplied, else its own
// random id. Advancing the shared sequence keeps siblings distinct without the
// caller having to know how many components an answer contains.
func instanceIDFor(d *Directive, idPrefix string, seq *int) string {
	if id := d.InstanceID(); validInstanceID(id) {
		return id
	}
	if idPrefix != "" {
		*seq++
		if id := fmt.Sprintf("%s%d", idPrefix, *seq); validInstanceID(id) {
			d.ensureInstanceID(id)
			return id
		}
	}
	d.ensureInstanceID("")
	return d.InstanceID()
}
