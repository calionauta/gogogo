// SCOPE:feature — the search seam's READ half: the wired layers and the
// per-request mode choice.
//
// Split from chi.go for the file-size gate, and along a real seam: chi.go holds
// the Handler and its wiring, this file holds the vocabulary the search box
// selects from. The interfaces are stdlib-only on purpose (no internal/search
// import) so trimming the search plugin leaves this file compiling — the plugin
// is injected through these types.
package handlers

import (
	"context"
	"sort"
)

// Searcher is the read half of the search seam: text in, ranked ids out.
// The query is owner-scoped by the implementation, and a caller narrows an
// already visibility-scoped list by these ids — so search can never widen
// what the viewer is allowed to see.
type Searcher interface {
	SearchIDs(ctx context.Context, owner, query string) ([]string, error)
}

// SearchLayers is the wired set of search layers, keyed by mode name, with the
// default the operator chose at boot.
//
// A map rather than one Searcher because the mode is a per-request choice: the
// consumer asks for what the tab currently has selected and gets an error only
// for a name that is not wired. The names come from internal/search's own
// registry, so this type never spells them.
type SearchLayers struct {
	// ByMode holds one Searcher per wired layer. Empty means the plugin is
	// trimmed (or never wired): no box, no search.
	ByMode map[string]Searcher
	// Default is the mode a tab starts on, from SEARCH_MODE. Always a key of
	// ByMode when ByMode is non-empty (boot asserts it).
	Default string
	// Order is the presentation order for the selector (cheap to strongest),
	// supplied by boot from the plugin's own Modes(). Empty means "no stated
	// order" and Modes() falls back to a sorted listing.
	Order []string
}

// Enabled reports whether any layer is wired.
func (l SearchLayers) Enabled() bool { return len(l.ByMode) > 0 }

// For resolves the layer for a requested mode, falling back to the default
// when the request names something unwired. Never nil for an enabled set: the
// fallback is what keeps a stale tab (holding a mode the operator removed)
// searching instead of erroring.
func (l SearchLayers) For(mode string) (Searcher, string) {
	if s, ok := l.ByMode[mode]; ok {
		return s, mode
	}
	if s, ok := l.ByMode[l.Default]; ok {
		return s, l.Default
	}
	for _, m := range l.Modes() {
		if s, ok := l.ByMode[m]; ok {
			return s, m
		}
	}
	return nil, ""
}

// Modes lists the wired mode names in the boot-provided PRESENTATION order
// (cheap to strongest: substring, fuzzy, bm25), which is what the selector
// renders.
//
// The order is a FIELD rather than something this package sorts, for two
// reasons. The canonical sequence lives in internal/search (the plugin owns its
// own layer vocabulary), and this package must NOT import that — it holds
// stdlib-only interfaces so trimming the plugin leaves it compiling. So boot,
// which legitimately holds both, supplies the order; this method just reports
// the wired subset of it.
//
// Alphabetical sorting was the first cut and put bm25 first: the picker would
// lead with the most expensive, least-literal layer, ranking a user's first
// search by relevance before they asked for it.
func (l SearchLayers) Modes() []string {
	if len(l.ByMode) == 0 {
		return nil
	}
	names := make([]string, 0, len(l.ByMode))
	for _, name := range l.Order {
		if _, ok := l.ByMode[name]; ok {
			names = append(names, name)
		}
	}
	// Fallback for a caller that built the set without an order (a test
	// fixture): report what is wired rather than nothing, sorted for
	// determinism. Boot always sets Order, so this only covers tests.
	if len(names) == 0 {
		for name := range l.ByMode {
			names = append(names, name)
		}
		sort.Strings(names)
	}
	return names
}

// activeSearchMode resolves the layer actually in use for a requested mode, so
// the selector reflects reality (the default, or the fallback) rather than the
// request. Returns "" when no layer is wired.
func (h *Handler) activeSearchMode(requested string) string {
	_, mode := h.Search.For(requested)
	return mode
}
