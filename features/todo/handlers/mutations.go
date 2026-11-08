// HTTP todo mutations (create/toggle/delete/clear)
package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/calionauta/gogogo/features/todo/components"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/features/todo"
	"github.com/calionauta/gogogo/features/todo/service"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
)

// createVisibility reads the create form's Shared checkbox: checked posts
// "public", unchecked posts nothing (private default — the pre-sharing
// behaviour). Anything else reads as private, never as shared.
func createVisibility(r *http.Request) string {
	if r.FormValue("visibility") == todo.VisibilityPublic {
		return todo.VisibilityPublic
	}
	return todo.VisibilityPrivate
}

func (h *Handler) handleCreate(w http.ResponseWriter, r *http.Request) {
	owner, err := h.ownerID(r)
	if err != nil {
		redirectLogin(w, r)
		return
	}
	if ferr := r.ParseForm(); ferr != nil {
		writeText(w, http.StatusBadRequest, "invalid form")
		return
	}
	// A card action's failures must land WHERE THE CLICK HAPPENED. A plain-text
	// 400 is invisible to the user when the form was submitted from a model
	// authored card by `@post` (Datastar shows nothing for an error response
	// body), so the button would look dead — the same class of bug as answering
	// a successful write on the wrong page. A card gets an in-place failure
	// card; every other caller keeps the text response it already expects.
	target := formTarget(r)
	title := r.FormValue("title")
	if title == "" {
		if target != "" {
			h.answerCardFailure(w, r, target, "A todo needs a title.")
			return
		}
		writeText(w, http.StatusBadRequest, "title required")
		return
	}
	visibility := createVisibility(r)
	idemKey := r.FormValue("idem_key")
	if idemKey == "" {
		idemKey = uuid.New().String()
	}

	// Idempotent replay, indexed create, onboarding resume and Bus
	// fan-out live in the domain service (shared with cmd/gui).
	ctx := r.Context()
	if _, cerr := h.svc().Create(ctx, owner, title, visibility, idemKey); cerr != nil {
		switch {
		case errors.Is(cerr, service.ErrTitleRequired):
			if target != "" {
				h.answerCardFailure(w, r, target, "A todo needs a title.")
				return
			}
			writeText(w, http.StatusBadRequest, "title required")
		case errors.Is(cerr, service.ErrInvalidIdemKey):
			if target != "" {
				h.answerCardFailure(w, r, target, "That request could not be read. Try again.")
				return
			}
			writeText(w, http.StatusBadRequest, "invalid idem_key")
		default:
			for _, marker := range []string{"dedupe lookup", "save"} {
				if strings.Contains(cerr.Error(), marker) {
					h.fail(r.Context(), w, "todo: "+marker+" failed", cerr, "save failed")
					return
				}
			}
			h.fail(r.Context(), w, "todo: save failed", cerr, "save failed")
		}
		return
	}

	if r.Header.Get("X-Offline-Replay") == "1" {
		writeText(w, http.StatusOK, "replayed")
		return
	}

	_, role := h.roleOf(r)
	todos, err := h.listVisible(ctx, owner, role, filterOf(r), scopeOf(r))
	if err != nil {
		h.fail(r.Context(), w, "todo: list after create failed", err, "error listing todos")
		return
	}
	h.respondCreated(ctx, w, r, todos, title)
}

// respondCreated streams the create's response: signals, the list patch, and —
// when the caller names its own slot — an in-place answer instead.
//
// Split from handleCreate for the complexity gate, and because the two halves
// answer different questions: the handler decides what to write, this decides
// how to report it.
//
// Two callers, two shapes, and the difference is what the FIRST of them can
// show. The todo page's composer owns #todo-list, so it gets the list patch and
// a short toast. A model-authored card (plan_cards' Apply on /chat)
// owns neither: it sends `_target`, so the response replaces THAT slot with the
// outcome and the list patch is skipped — aiming at a region the page does not
// have is exactly what made the click look inert while the row was created.
//
//nolint:contextcheck // SSE emission ends in datastar-go sinks (no ctx param)
func (h *Handler) respondCreated(
	ctx context.Context, w http.ResponseWriter, r *http.Request, todos []todo.Todo, title string,
) {
	sse := sdk.NewSSE(w, r)
	if err := dshelpers.MergeSignals(sse, map[string]any{
		// newTitle is deliberately NOT written here: $newTitle is bound
		// two-way to the composer (data-bind), so the box owns it. Writing
		// "" from this response wiped text the user had already typed after
		// submitting -- reproduced in the browser (type immediately after
		// Enter and the input goes empty), and worked around in the smoke by
		// waiting between creates. The client clears it after the request
		// succeeds, and only if it still holds what was sent.
		"loading": false, signalItemCount: len(todos),
	}); err != nil {
		h.fail(ctx, w, "todo: create signals failed", err, "save failed")
		return
	}
	// An action fired from a model-authored card names its OWN slot, and that
	// slot is the whole response. Reporting a card-level write with the page's
	// list patch is what produced "I clicked and nothing happened": the patch's
	// target is the todo list, which the page hosting the card does not have,
	// so the browser matched nothing and the card looked inert — while the row
	// was in fact created.
	if target := formTarget(r); target != "" {
		if aerr := h.answerCardAction(sse, target, title); aerr != nil {
			h.fail(ctx, w, "todo: action result patch failed", aerr, "save failed")
		}
		return
	}
	if err := h.patchListSelf(ctx, sse, todos, r); err != nil {
		h.fail(ctx, w, "todo: create patch failed", err, "save failed")
		return
	}
	if err := emitToast(sse, "Added", "success"); err != nil {
		h.fail(ctx, w, "todo: create toast failed", err, "save failed")
	}
}

// patchListSelf tags the originating mutation as "self" (self-origin entry
// animation) and patches the list with view transitions, merging the fresh
// item count in the same SSE response.
//
// The re-list narrows by the tab's ACTIVE SEARCH (read from its stored view
// prefs, since a mutation URL carries no `q`): without this, pressing toggle
// or delete while a search is active would re-render every row the search had
// excluded. The stored query is the same authority the record-event re-list
// reads, so one tab cannot end up on a different slice than another.
//
// The $searchQuery SIGNAL is deliberately not written here — the box owns it
// (see displaySignals), and the field is `json:"-"`, so a merge carries no key
// for it at all and the client's live text is untouched.
//
//nolint:contextcheck // SSE emission ends in datastar-go sinks, which take no
func (h *Handler) patchListSelf(
	ctx context.Context, sse *sdk.ServerSentEventGenerator, todos []todo.Todo, r *http.Request,
) error {
	// Read the tab's stored view ONCE: two ViewOf calls take two separate read
	// locks, so a concurrent SetView (the same tab issuing another fetch) could
	// be observed between them and the query and mode would come from different
	// snapshots — a search narrowed by one mode and labelled by another.
	view := h.Hub.ViewOf(r.URL.Query().Get("clientID"))
	if owner, err := h.ownerID(r); err == nil {
		narrowed, nerr := h.narrowBySearch(ctx, owner, view.Query, view.Mode, todos)
		if nerr != nil {
			return nerr
		}
		todos = narrowed
	}
	if err := dshelpers.MergeSignals(sse, map[string]any{
		"lastItemSource": "self",
		signalItemCount:  len(todos),
	}); err != nil {
		return err
	}
	// displaySignals never sets SearchQuery (the box owns it), so a mutation
	// cannot clobber live input. The rows still narrow by the STORED query
	// above, so the slice stays correct without touching the signal.
	signals := h.displaySignals(r, todos, filterOf(r), scopeOf(r))
	return patchSSE(sse, components.TodoListRegion(signals),
		sdk.WithSelector("#todo-list"),
		sdk.WithViewTransitions())
}

// context; disconnects surface as write errors, checked per call.
func (h *Handler) handleToggle(w http.ResponseWriter, r *http.Request) {
	owner, err := h.ownerID(r)
	if err != nil {
		redirectLogin(w, r)
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	// trim-genui:panel:start (toggle target)
	target := formTarget(r) // trim-genui:panel:end
	opKey := idempotencyKey(r)
	_, applied, terr := h.svc().Toggle(ctx, owner, id, opKey)
	if terr != nil {
		switch {
		case errors.Is(terr, service.ErrNotFound):
			// trim-genui:panel:start (toggle read)
			if h.answerPanelReadErr(w, r, owner, target, id, "toggle", terr) {
				return
			} // trim-genui:panel:end
			writeText(w, http.StatusNotFound, "not found")
		default:
			// trim-genui:panel:start (toggle save)
			if h.answerPanelWriteErr(w, r, target,
				"todo: toggle save failed", terr, "Couldn't save that change.") {
				return
			} // trim-genui:panel:end
			h.fail(r.Context(), w, "todo: toggle save failed", terr, "toggle failed")
		}
		return
	}
	if !applied {
		writeText(w, http.StatusOK, "replayed")
		return
	}
	if r.Header.Get("X-Offline-Replay") == "1" {
		writeText(w, http.StatusOK, "replayed")
		return
	}
	// trim-genui:panel:start (toggle outcome)
	h.answerToggleOutcome(ctx, w, r, owner, target) // trim-genui:panel:end
}

func (h *Handler) handleConfirmDelete(w http.ResponseWriter, r *http.Request) {
	owner, err := h.ownerID(r)
	if err != nil {
		redirectLogin(w, r)
		return
	}
	id := chi.URLParam(r, "id")
	row, err := h.Q.GetTodoVisible(r.Context(), dbgen.GetTodoVisibleParams{ID: id, Owner: owner})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeText(w, http.StatusNotFound, "not found")
			return
		}
		h.fail(r.Context(), w, "todo: confirm-delete read failed", err, "delete failed")
		return
	}
	item, err := toVisibleOne(row)
	if err != nil {
		h.fail(r.Context(), w, "todo: confirm-delete map failed", err, "delete failed")
		return
	}
	// Same rule as the destructive act (canDeleteRow, enforced here): the
	// modal wiring is only handed out for rows the requester may delete.
	_, role := h.roleOf(r)
	if !canDeleteRow(role, owner, item) {
		writeText(w, http.StatusForbidden, "cannot delete this todo")
		return
	}
	sse := sdk.NewSSE(w, r)
	if err := dshelpers.MergeSignals(sse, map[string]any{
		"confirmingDeleteId":    item.ID,
		"confirmingDeleteTitle": item.Title,
	}); err != nil {
		h.fail(r.Context(), w, "todo: confirm-delete signals failed", err, "delete failed")
	}
}

// context; disconnects surface as write errors, checked per call.
func (h *Handler) handleDelete(w http.ResponseWriter, r *http.Request) {
	owner, err := h.ownerID(r)
	if err != nil {
		redirectLogin(w, r)
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	target := formTarget(r) // trim-genui:panel:end
	_, role := h.roleOf(r)
	item, derr := h.svc().Delete(ctx, owner, role, id)
	if derr != nil {
		switch {
		case errors.Is(derr, service.ErrNotFound):
			if h.answerPanelReadErr(w, r, owner, target, id, "delete", derr) {
				return
			}
			writeText(w, http.StatusNotFound, "not found")
		case errors.Is(derr, service.ErrForbidden):
			if h.answerPanelWriteErr(w, r, target,
				"todo: panel delete forbidden", derr, "You can't delete this todo.") {
				return
			}
			writeText(w, http.StatusForbidden, "viewer role cannot delete")
		default:
			if h.answerPanelWriteErr(w, r, target,
				"todo: delete failed", derr, "Couldn't delete that todo.") {
				return
			}
			h.fail(r.Context(), w, "todo: delete failed", derr, "delete failed")
		}
		return
	}
	if r.Header.Get("X-Offline-Replay") == "1" {
		writeText(w, http.StatusOK, "replayed")
		return
	}
	// trim-genui:panel:start (delete outcome)
	h.answerDeleteOutcome(ctx, w, r, owner, role, target, item.Title) // trim-genui:panel:end
}

// context; disconnects surface as write errors, checked per call.
//
//nolint:contextcheck // SSE emission ends in datastar-go sinks, which take no
func (h *Handler) handleClearCompleted(w http.ResponseWriter, r *http.Request) {
	owner, err := h.ownerID(r)
	if err != nil {
		redirectLogin(w, r)
		return
	}
	ctx := r.Context()
	count, err := h.svc().ClearCompleted(ctx, owner)
	if err != nil {
		h.fail(r.Context(), w, "todo: clear completed failed", err, "clear failed")
		return
	}
	if r.Header.Get("X-Offline-Replay") == "1" {
		writeText(w, http.StatusOK, "replayed")
		return
	}

	_, role := h.roleOf(r)
	todos, err := h.listVisible(ctx, owner, role, filterOf(r), scopeOf(r))
	if err != nil {
		h.fail(r.Context(), w, "todo: list after clear failed", err, "error listing todos")
		return
	}
	sse := sdk.NewSSE(w, r)
	if err := h.patchListSelf(ctx, sse, todos, r); err != nil {
		h.fail(r.Context(), w, "todo: clear patch failed", err, "clear failed")
		return
	}
	if count == 0 {
		if err := emitToast(sse, "Nothing to clear", "info"); err != nil {
			h.fail(r.Context(), w, "todo: clear toast failed", err, "clear failed")
		}
		return
	}
	if err := emitToast(sse, fmt.Sprintf("Cleared %d completed", count), "success"); err != nil {
		h.fail(r.Context(), w, "todo: clear toast failed", err, "clear failed")
	}
}
