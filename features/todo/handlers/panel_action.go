// SCOPE:feature — a native panel's own row actions, answered in place.
//
// Split from mutations.go for the file-size gate, and along a real seam:
// that file owns what a WRITE does (store, fan-out, page patch), this one
// owns what a write FIRED FROM A PANEL reports — the slot it names, the
// slice it carries, and the in-place re-render or failure card.
//
// WHY THIS EXISTS. A panel renders on pages this feature does not own
// (a chat answer), so the page patch mutations already send — a re-list
// aimed at #todo-list — matches nothing there and the click reads as dead
// while the row was in fact written (the same class of bug card actions
// had, fixed the same way: answer where the click happened). A panel row
// therefore posts its own slot id as `_target` plus its filter slice, and
// the routes below re-render THIS panel from the store instead of the page.
//
// GENERIC ON PURPOSE. Nothing here knows what a chat is, and nothing in
// features/genui/components knows what a todo is: the seam is the `_target`
// convention catalog cards already use (a validated slot id, an in-place
// patch via RenderAndPatch with that selector). A future panel gets
// interactivity the same way — render forms posting to its own routes with
// `_target`, answer them by re-rendering through its own provider — with no
// shared-code change. The fill below is duplicated from the provider rather
// than calling it so this file (and the mutations that branch into it)
// keeps compiling when the genui plugin is trimmed.
package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/features/todo"
	todoscomp "github.com/calionauta/gogogo/features/todo/components"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	"github.com/calionauta/gogogo/internal/observability"
)

// panelFilterOf reads the panel slice the action was fired from. Posted as a
// query param by the panel's own markup (like the page's filter tabs), so
// r.URL.Query — not the form body — is authoritative. Unknown reads as all:
// the same degrade rule as scopeOf (a typo shows the full own list, never
// another tenant's rows, which the store scopes out before Go sees them).
func panelFilterOf(r *http.Request) string {
	f := todo.NormalizeFilter(r.URL.Query().Get("filter"))
	if !todo.FilterValid(f) {
		return todo.FilterAll
	}
	return f
}

// panelRows is the store half of the panel's fill: the viewer's own rows,
// narrowed to the closed-enum slice. Same store call the provider uses
// (ListTodosByOwner — own rows only), so the re-render shows the truth the
// mutation just wrote and invents nothing.
func (h *Handler) panelRows(ctx context.Context, owner, filter string) ([]todo.PanelRows, error) {
	rows, err := h.Q.ListTodosByOwner(ctx, owner)
	if err != nil {
		return nil, err
	}
	out := make([]todo.PanelRows, 0, len(rows))
	for _, r := range rows {
		switch filter {
		case todo.FilterOpen:
			if r.Completed {
				continue
			}
		case todo.FilterDone:
			if !r.Completed {
				continue
			}
		}
		out = append(out, todo.PanelRows{ID: r.ID, Title: r.Title, Done: r.Completed})
	}
	return out, nil
}

// answerPanelSelf re-renders one native panel instance in place: rows come
// from the store (never the request), so the acting tab shows the truth it
// just wrote. A store failure answers the failure card at the same slot —
// a 500 text body renders nowhere when the request came from a panel's
// @post, so the plain-text failure the page path uses would read as a dead
// control here.
//
//nolint:contextcheck // SSE emission ends in datastar-go sinks (no ctx param)
func (h *Handler) answerPanelSelf(w http.ResponseWriter, r *http.Request, owner, target string) error {
	filter := panelFilterOf(r)
	rows, err := h.panelRows(r.Context(), owner, filter)
	if err != nil {
		h.panelFailure(r.Context(), w, r, target,
			"todo: panel re-list failed", err, "Couldn't reload this list. Open Todos to see it.")
		return nil
	}
	sse := sdk.NewSSE(w, r)
	return patchSSE(sse, todoscomp.Panel(target, filter, rows), sdk.WithSelector("#"+target))
}

// panelFailure logs the cause and answers the failure card at the clicked
// slot: a panel's @post renders nothing for an error-status body, so the
// plain-text failures the page path uses would read as a dead control here.
func (h *Handler) panelFailure(
	ctx context.Context, w http.ResponseWriter, r *http.Request,
	target, logMsg string, err error, publicMsg string,
) {
	obsArgs := append(observability.WithComponent("todo", "feature"), "error", err)
	observability.Error(ctx, logMsg, obsArgs...)
	h.answerCardFailure(w, r, target, publicMsg)
}

// answerPanelReadErr answers a store-read failure for a panel post: gone
// rows refresh to the current truth (the same a reload would show), other
// failures get the in-place card. Reports whether it answered (target set);
// the page path runs otherwise, byte-identical to before.
func (h *Handler) answerPanelReadErr(
	w http.ResponseWriter, r *http.Request, owner, target, id, op string, err error,
) bool {
	if target == "" {
		return false
	}
	ctx := r.Context()
	if errors.Is(err, sql.ErrNoRows) {
		if perr := h.answerPanelSelf(w, r, owner, target); perr != nil {
			h.fail(ctx, w, "todo: "+op+" panel patch failed", perr, op+" failed")
		}
		return true
	}
	h.panelFailure(ctx, w, r, target,
		"todo: "+op+" read failed", fmt.Errorf("id %s: %w", id, err), "Couldn't read that todo.")
	return true
}

// answerPanelWriteErr answers a store-write or auth failure for a panel
// post with the in-place card. Reports whether it answered (target set);
// the page path runs otherwise, byte-identical to before.
func (h *Handler) answerPanelWriteErr(
	w http.ResponseWriter, r *http.Request, target, logMsg string, err error, publicMsg string,
) bool {
	if target == "" {
		return false
	}
	h.panelFailure(r.Context(), w, r, target, logMsg, err, publicMsg)
	return true
}

// answerToggleOutcome answers a toggle: the panel in place, or the page
// list. Split from handleToggle for the complexity gate; the page path is
// the same re-list plus patch, untouched.
func (h *Handler) answerToggleOutcome(
	ctx context.Context, w http.ResponseWriter, r *http.Request, owner, target string,
) {
	if target != "" {
		if perr := h.answerPanelSelf(w, r, owner, target); perr != nil {
			h.fail(ctx, w, "todo: toggle panel patch failed", perr, "toggle failed")
		}
		return
	}
	_, role := h.roleOf(r)
	todos, err := h.listVisible(ctx, owner, role, filterOf(r), scopeOf(r))
	if err != nil {
		h.fail(ctx, w, "todo: list after toggle failed", err, "error listing todos")
		return
	}
	sse := sdk.NewSSE(w, r)
	if err := h.patchListSelf(ctx, sse, todos, r); err != nil {
		h.fail(ctx, w, "todo: toggle patch failed", err, "toggle failed")
	}
}

// answerDeleteOutcome answers a delete: the panel minus the row in place,
// or the page list plus its confirm-reset and toast. Split from
// handleDelete for the complexity gate; the page path is untouched.
// no context; disconnects surface as write errors.
//
//nolint:contextcheck // SSE emission ends in datastar-go sinks, which take
func (h *Handler) answerDeleteOutcome(
	ctx context.Context, w http.ResponseWriter, r *http.Request,
	owner, role, target, title string,
) {
	if target != "" {
		if perr := h.answerPanelSelf(w, r, owner, target); perr != nil {
			h.fail(ctx, w, "todo: delete panel patch failed", perr, "delete failed")
		}
		return
	}
	todos, err := h.listVisible(ctx, owner, role, filterOf(r), scopeOf(r))
	if err != nil {
		h.fail(ctx, w, "todo: list after delete failed", err, "error listing todos")
		return
	}
	sse := sdk.NewSSE(w, r)
	if err := dshelpers.MergeSignals(sse, map[string]any{
		"confirmingDeleteId":    "",
		"confirmingDeleteTitle": "",
	}); err != nil {
		h.fail(ctx, w, "todo: delete signals failed", err, "delete failed")
		return
	}
	if err := h.patchListSelf(ctx, sse, todos, r); err != nil {
		h.fail(ctx, w, "todo: delete patch failed", err, "delete failed")
		return
	}
	if err := emitToast(sse, fmt.Sprintf("Deleted “%s”", title), "info"); err != nil {
		h.fail(ctx, w, "todo: delete toast failed", err, "delete failed")
	}
}
