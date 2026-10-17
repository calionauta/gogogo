// SCOPE:feature — the todo search slice: the read path honours the active
// query and the endpoint the search box fires.
//
// Split from chi.go for the file-size gate. To remove: delete with
// features/todo/ (the plugin's own half lives in internal/search).
package handlers

import (
	"context"
	"fmt"
	"net/http"

	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/features/todo"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
)

// searchQueryOf reads the tab's active search text. Unknown/empty is "no
// search": the list shows everything visible, exactly as before the plugin.
func searchQueryOf(r *http.Request) string {
	return r.URL.Query().Get("q")
}

// narrowBySearch reorders and filters an already-scoped list down to the
// ranked hits. `order` is the plugin's ranking, best first; a row missing
// from it is not a match. When Search is nil (plugin trimmed) or the query
// is empty, the list is returned untouched — the degrade path the plugin's
// own capability note promises.
//
// Rows are matched by rank, NOT re-filtered by re-running the layer: the
// caller already scoped the set (visibility + filter + scope), so this can
// only ever narrow it.
func (h *Handler) narrowBySearch(ctx context.Context, owner, query string, todos []todo.Todo) ([]todo.Todo, error) {
	if h.Search == nil || query == "" {
		return todos, nil
	}
	ids, err := h.Search.SearchIDs(ctx, owner, query)
	if err != nil {
		return nil, fmt.Errorf("search %q: %w", query, err)
	}
	if len(ids) == 0 {
		return []todo.Todo{}, nil
	}
	byID := make(map[string]todo.Todo, len(todos))
	for _, t := range todos {
		byID[t.ID] = t
	}
	out := make([]todo.Todo, 0, len(ids))
	for _, id := range ids {
		// A hit that is not in the caller's visible set is dropped, never
		// resurrected: the index is owner-scoped in SQL, and this second
		// gate keeps the handler's own visibility rule authoritative even
		// if a stale posting outlived its row.
		if t, ok := byID[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

// searchEnabled reports whether a search layer is wired, which is what the
// templates gate the input on: trimmed plugin, no box.
func (h *Handler) searchEnabled() bool { return h.Search != nil }

// handleSearch answers the search box: it re-lists the caller's visible set,
// narrowed to the ranked hits, and patches the same `#todo-list` region the
// filter tabs do. One region, one renderer — the results need no markup of
// their own, and the ranking shows up as the row order.
//
// The tab's query is remembered on its stream registration (SetView family),
// so a record-event re-list keeps the active search instead of silently
// showing everything again.
//
//nolint:contextcheck // SSE emission ends in datastar-go sinks, which take no
func (h *Handler) handleSearch(w http.ResponseWriter, r *http.Request) {
	owner, err := h.ownerID(r)
	if err != nil {
		redirectLogin(w, r)
		return
	}
	query := searchQueryOf(r)
	filter, scope := filterOf(r), scopeOf(r)
	h.Hub.SetView(r.URL.Query().Get("clientID"), filter, scope, query)
	_, role := h.roleOf(r)

	todos, err := h.listVisible(r.Context(), owner, role, filter, scope)
	if err != nil {
		h.fail(r.Context(), w, "todo: search list failed",
			fmt.Errorf("filter %s scope %s: %w", filter, scope, err), "error searching todos")
		return
	}
	todos, err = h.narrowBySearch(r.Context(), owner, query, todos)
	if err != nil {
		h.fail(r.Context(), w, "todo: search failed", err, "error searching todos")
		return
	}
	signals := h.displaySignals(r, todos, filter, scope)
	sse := sdk.NewSSE(w, r)
	if err := dshelpers.MergeSignals(sse, signals); err != nil {
		h.fail(r.Context(), w, "todo: search signals failed", err, "error searching todos")
		return
	}
	if err := patchSSE(
		sse, h.renderListRegion(signals, h.resolveSkin(r)),
		sdk.WithSelector("#todo-list"),
	); err != nil {
		h.fail(r.Context(), w, "todo: search patch failed", err, "error searching todos")
	}
}
