// SCOPE:feature — HTTP todo mutations (create/toggle/delete/clear)
package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/calionauta/gogogo/features/todo/components"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/features/todo"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
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
	} else if !validIdemKey(idemKey) {
		// The key is CLIENT-SUPPLIED and stored in a TEXT column, so an
		// unbounded value is a cheap way to write arbitrarily large rows (the
		// unique index keeps them distinct, so repeats do not collide). Any
		// honest client sends a UUID; the two bounds below admit that and every
		// browser-generated variant while refusing the abuse.
		if target != "" {
			h.answerCardFailure(w, r, target, "That request could not be read. Try again.")
			return
		}
		writeText(w, http.StatusBadRequest, "invalid idem_key")
		return
	}

	// Idempotent replay: a retried create with the same dedupe token
	// returns the original row instead of a duplicate.
	ctx := r.Context()
	createdID := ""
	dedupe, derr := h.Q.GetTodoByIdemKey(ctx, dbgen.GetTodoByIdemKeyParams{
		Owner: owner, IdemKey: idemKey,
	})
	switch {
	case derr == nil:
		createdID = dedupe.ID // replay: the original row stands.
	case !errors.Is(derr, sql.ErrNoRows):
		h.fail(r.Context(), w, "todo: dedupe lookup failed", derr, "save failed")
		return
	default:
		now := dbschema.Now()
		newID := dbschema.NewID()
		if cerr := createTodoIndexed(ctx, h.Q, h.Index, h.DB, dbgen.CreateTodoParams{
			ID: newID, Owner: owner, Title: title,
			Completed: false, IdemKey: idemKey, Visibility: visibility,
			Created: now, Updated: now,
		}); cerr != nil {
			// A concurrent create may win the race on the partial unique
			// index: re-read instead of 500ing a row that now exists.
			if existing, rerr := h.Q.GetTodoByIdemKey(ctx, dbgen.GetTodoByIdemKeyParams{
				Owner: owner, IdemKey: idemKey,
			}); rerr == nil {
				createdID = existing.ID
			} else {
				h.fail(r.Context(), w, "todo: save failed", cerr, "save failed")
				return
			}
		} else {
			createdID = newID
		}
	}

	// Resume the durable onboarding workflow: the first todo a user with a
	// pending run creates is the signal the blocked WaitForSignal step is
	// waiting for. Nil (engine disabled) is a no-op.
	if h.Onboarding != nil {
		h.Onboarding.ResumeOnboarding(owner)
	}

	// Fan-out precedes the replay short-circuit below: a replayed request
	// answers "replayed" with no SSE body, so the hub broadcast (plus the
	// JetStream op) is the only path live tabs learn of the change through.
	// The previous stack fanned out at the record-write layer, independent
	// of the HTTP response; this stack fans out here, so it must not sit
	// behind the early return.
	h.publish(r.Context(), r, todo.Event{
		Kind: todo.Created, ID: createdID, Title: title, Owner: owner, Visibility: visibility,
	})

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
	// A panel row posts its own slot id as _target: the answer re-renders
	// that panel in place (see panel_action.go) instead of the page list
	// the host page does not have.
	target := formTarget(r) // trim-genui:panel:end
	// Visibility-scoped read: own rows plus public rows. A private row of
	// another user reads as "not found" — indistinguishable from missing,
	// so existence never leaks across tenants.
	row, err := h.Q.GetTodoVisible(ctx, dbgen.GetTodoVisibleParams{ID: id, Owner: owner})
	if err != nil {
		// trim-genui:panel:start (toggle read)
		if h.answerPanelReadErr(w, r, owner, target, id, "toggle", err) {
			return
		} // trim-genui:panel:end
		if errors.Is(err, sql.ErrNoRows) {
			writeText(w, http.StatusNotFound, "not found")
			return
		}
		h.fail(r.Context(), w, "todo: toggle read failed", fmt.Errorf("id %s: %w", id, err), "toggle failed")
		return
	}
	current, err := toVisibleOne(row)
	if err != nil {
		h.fail(r.Context(), w, "todo: toggle map failed", fmt.Errorf("id %s: %w", id, err), "toggle failed")
		return
	}
	now := dbschema.Now()
	// The toggle is a read-then-flip, so it is NOT idempotent: a replayed
	// intent (the outbox resends after a lost response) would flip the row
	// straight back. applyOnce claims the intent's key in the same
	// transaction as the update, so the second delivery is a no-op.
	applied, serr := h.applyOnce(r, owner, func(ctx context.Context, q *dbgen.Queries) error {
		return q.SetTodoCompletedByID(ctx, dbgen.SetTodoCompletedByIDParams{
			Completed: !current.Completed, Updated: now, ID: id,
		})
	})
	if serr != nil {
		// trim-genui:panel:start (toggle save)
		serr = fmt.Errorf("id %s: %w", id, serr)
		if h.answerPanelWriteErr(w, r, target,
			"todo: toggle save failed", serr, "Couldn't save that change.") {
			return
		} // trim-genui:panel:end
		h.fail(r.Context(), w, "todo: toggle save failed", serr, "toggle failed")
		return
	}
	if !applied {
		// The first delivery already applied this intent and published its
		// record event. A second patch or broadcast would duplicate both.
		writeText(w, http.StatusOK, "replayed")
		return
	}

	// Fan-out before the replay short-circuit (see handleCreate): the
	// "replayed" acknowledgment carries no patch, so the broadcast is the
	// only path live tabs learn of the toggle through.
	visibility := todo.VisibilityPrivate
	if current.IsShared() {
		visibility = todo.VisibilityPublic
	}
	h.publish(r.Context(), r, todo.Event{
		Kind: todo.Updated, ID: id, Done: !current.Completed, Owner: owner, Visibility: visibility,
	})

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
	// trim-genui:panel:start (delete target)
	// A panel row posts its own slot id as _target (see panel_action.go):
	// errors and the outcome all land on that slot, never on the page list
	// the host page does not have.
	target := formTarget(r) // trim-genui:panel:end
	row, err := h.Q.GetTodoVisible(ctx, dbgen.GetTodoVisibleParams{ID: id, Owner: owner})
	if err != nil {
		// trim-genui:panel:start (delete read)
		if h.answerPanelReadErr(w, r, owner, target, id, "delete", err) {
			return
		} // trim-genui:panel:end
		if errors.Is(err, sql.ErrNoRows) {
			writeText(w, http.StatusNotFound, "not found")
			return
		}
		h.fail(r.Context(), w, "todo: delete read failed", fmt.Errorf("id %s: %w", id, err), "delete failed")
		return
	}
	item, err := toVisibleOne(row)
	if err != nil {
		h.fail(r.Context(), w, "todo: delete map failed", fmt.Errorf("id %s: %w", id, err), "delete failed")
		return
	}
	// Delete rule (mirrors canDeleteRow, enforced here — the modal affordance
	// is display-only): viewer never deletes, even their own row; owner
	// deletes own; admin deletes any visible row.
	_, role := h.roleOf(r)
	if !canDeleteRow(role, owner, item) {
		// trim-genui:panel:start (delete forbidden)
		if h.answerPanelWriteErr(w, r, target,
			"todo: panel delete forbidden", fmt.Errorf("role %s id %s", role, id), "You can't delete this todo.") {
			return
		} // trim-genui:panel:end
		writeText(w, http.StatusForbidden, "viewer role cannot delete")
		return
	}
	if derr := deleteTodoIndexed(ctx, h.Q, h.Index, h.DB, owner, item.ID); derr != nil {
		// trim-genui:panel:start (delete write)
		derr = fmt.Errorf("id %s: %w", id, derr)
		if h.answerPanelWriteErr(w, r, target,
			"todo: delete failed", derr, "Couldn't delete that todo.") {
			return
		} // trim-genui:panel:end
		h.fail(r.Context(), w, "todo: delete failed", derr, "delete failed")
		return
	}

	// Fan-out before the replay short-circuit (see handleCreate): the
	// "replayed" acknowledgment carries no patch, so the broadcast is the
	// only path live tabs learn of the delete through.
	visibility := todo.VisibilityPrivate
	if item.IsShared() {
		visibility = todo.VisibilityPublic
	}
	h.publish(r.Context(), r, todo.Event{
		Kind: todo.Deleted, ID: item.ID, Owner: owner, Visibility: visibility,
	})

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
	count, err := clearCompletedIndexed(ctx, h.Q, h.Index, h.DB, owner)
	if err != nil {
		h.fail(r.Context(), w, "todo: clear completed failed", err, "clear failed")
		return
	}

	// Fan-out before the replay short-circuit (see handleCreate): the
	// "replayed" acknowledgment carries no patch, so the broadcast is the
	// only path live tabs learn of the clear through. Clearing deletes only
	// the requester's own completed rows, so the audience stays owner-scoped
	// even when shared rows exist.
	h.publish(r.Context(), r, todo.Event{
		Kind: todo.Cleared, Owner: owner, Visibility: todo.VisibilityPrivate,
	})

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
