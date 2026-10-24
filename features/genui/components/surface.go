// SCOPE:plugin — the answer-surface declaration for the Chat demo.
//
// Chat is the one AI demo: one surface set, one interaction shape
// (persisted conversation). The Ask one-shot leg is deleted; its
// demonstrable strictness (catalog-only, fail-closed repair) lives in Chat
// through the surface set an operator toggles, not a second page.
//
// Three surfaces exist, and the split that matters is WHO OWNS THE CONTENT:
//
//	catalog — the fixed registry. The model picks a component name and fills
//	          typed props. Static: nothing in the answer depends on app state.
//	custom  — html_block. The model composes markup for this request, sanitized
//	          server-side and limited to a safelisted class vocabulary. This is
//	          the only surface whose output the model invents wholesale, which
//	          is why it is the one worth toggling.
//	native  — system panels. The APP owns the content (real rows from the
//	          store); the model picks only the SLICE. Always on: it is the
//	          surface that makes an answer about the user's own data correct
//	          instead of plausible, and there is no reason for a deployment to
//	          want a worse answer. See panel.go.
//
// Surfaces are a property of the demo configuration, not of a message: a
// conversation that sometimes allows invented markup and sometimes does not
// would be a state machine with no user-facing explanation.
package components

import (
	"fmt"
	"slices"
	"strings"
)

// Surface names one band of the answer vocabulary.
//
// A string-backed type, not a struct or a bitmask, on purpose: the values are
// read from an env var, written to logs, and compared against registry
// entries, so a string is what every one of those boundaries already speaks.
// The type is what makes the ENUM part real — a bare string could not be
// constrained at all — and Valid() plus ParseSurfaces are the two places that
// check membership (see ValidateSurfaces).
type Surface string

const (
	// SurfaceCatalog is the fixed generative registry (renderers).
	SurfaceCatalog Surface = "catalog"
	// SurfaceCustom is html_block: model-invented, sanitized markup.
	SurfaceCustom Surface = "custom"
	// SurfaceNative is the app's own panels (panels / PanelProvider).
	SurfaceNative Surface = "native"
)

// Valid reports whether this is one of the three declared bands. It is the
// membership test the env parser and ValidateSurfaces share, so "what counts
// as a surface" is declared once, next to the constants.
func (s Surface) Valid() bool { return slices.Contains(allSurfaces, s) }

// allSurfaces is the closed set, in advertisement order. Order is deliberate:
// custom leads when enabled, because the two legs read as the same feature
// when the catalog is listed first (see PromptForChat's history).
var allSurfaces = []Surface{SurfaceCustom, SurfaceCatalog, SurfaceNative}

// DefaultSurfacesChat is Chat's default: the catalog plus the invented-markup
// escape hatch, plus native (appended by NormalizeSurfaces).
var DefaultSurfacesChat = []Surface{SurfaceCatalog, SurfaceCustom}

// ParseSurfaces reads a CSV surface list (e.g. "catalog,custom"), or returns
// the default when the value is empty.
//
// An unknown name is an ERROR, not a silent drop: a typo in a variable that
// controls what the model may emit would otherwise change the answer surface
// with no signal at all, and the whole point of the variable is operator
// control. An empty result (every name filtered out) is also an error — an
// answer with no surface has no prompt.
func ParseSurfaces(csv string, def []Surface) ([]Surface, error) {
	if strings.TrimSpace(csv) == "" {
		return NormalizeSurfaces(def), nil
	}
	seen := make(map[Surface]bool, len(allSurfaces))
	var out []Surface
	for raw := range strings.SplitSeq(csv, ",") {
		name := Surface(strings.ToLower(strings.TrimSpace(raw)))
		if name == "" {
			continue
		}
		if !name.Valid() {
			return nil, fmt.Errorf("genui: unknown surface %q (valid: %s)", string(name), surfaceNames())
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("genui: no surface enabled (valid: %s)", surfaceNames())
	}
	return NormalizeSurfaces(out), nil
}

// ValidateSurfaces reports the first value in the set that is not a declared
// band.
//
// NormalizeSurfaces DROPS an unknown value (it is a normalizer, and silently
// dropping garbage is the only thing it can do without an error return), so a
// Go caller building a set from literals — as opposed to the env path, which
// ParseSurfaces already rejects — could otherwise hand a leg a vocabulary it
// cannot name, and the only symptom would be that the band is missing from the
// prompt. That is a fail-OPEN shape in a repo that fails closed, so the
// composition root asserts the invariant once and a test pins it.
func ValidateSurfaces(set []Surface) error {
	for _, s := range set {
		if !s.Valid() {
			return fmt.Errorf("genui: unknown surface %q (valid: %s)", string(s), surfaceNames())
		}
	}
	return nil
}

// NormalizeSurfaces returns surfaces in advertisement order, de-duplicated,
// with native always present (it is not an operator choice — see the file
// comment) and an empty input falling back to the catalog so no caller can
// produce an empty vocabulary by passing nil. Unknown values are dropped;
// ValidateSurfaces is what reports them.
func NormalizeSurfaces(in []Surface) []Surface {
	seen := make(map[Surface]bool, len(allSurfaces))
	for _, s := range in {
		seen[s] = true
	}
	seen[SurfaceNative] = true
	if len(seen) == 1 && seen[SurfaceNative] {
		seen[SurfaceCatalog] = true
	}
	out := make([]Surface, 0, len(allSurfaces))
	for _, s := range allSurfaces {
		if seen[s] {
			out = append(out, s)
		}
	}
	return out
}

// HasSurfaces reports whether every named surface is enabled in set.
func HasSurfaces(set []Surface, names ...Surface) bool {
	for _, n := range names {
		if !slices.Contains(set, n) {
			return false
		}
	}
	return true
}

// Names renders a surface list for logs and docs ("custom, catalog, native").
func Names(set []Surface) string {
	parts := make([]string, 0, len(set))
	for _, s := range set {
		parts = append(parts, string(s))
	}
	return strings.Join(parts, ", ")
}

// surfaceNames is the valid-values list for error messages.
func surfaceNames() string {
	parts := make([]string, 0, len(allSurfaces))
	for _, s := range allSurfaces {
		parts = append(parts, string(s))
	}
	return strings.Join(parts, ", ")
}

// Mode is the interaction shape — the difference that actually distinguishes
// the two legs once the surface set is shared.
type Mode string

const (
	// ModeOneShot answers one request into one region: no history, no
	// follow-up, and a shape error is loud (Ask).
	ModeOneShot Mode = "one-shot"
	// ModeConversation persists turns per owner and degrades an unshaped
	// answer to prose instead of failing the message (Chat).
	ModeConversation Mode = "conversation"
)
