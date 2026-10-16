// SCOPE:feature — HTTP todo mutations (create/toggle/delete/clear)
package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/features/todo"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/natswire"
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

// context; disconnects surface as write errors, checked per call.
//
//nolint:contextcheck // SSE emission ends in datastar-go sinks, which take no
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
	title := r.FormValue("title")
	if title == "" {
		writeText(w, http.StatusBadRequest, "title required")
		return
	}
	visibility := createVisibility(r)
	idemKey := r.FormValue("idem_key")
	if idemKey == "" {
		idemKey = uuid.New().String()
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
	h.publishSync(r, owner, visibility == todo.VisibilityPublic,
		recordEvent{Event: "created", ID: createdID, Title: title},
		natswire.CrudOpCreate, &natswire.CrudOpData{ID: createdID, Title: title, Visibility: visibility})

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
	sse := sdk.NewSSE(w, r)
	if err := dshelpers.MergeSignals(sse, map[string]any{
		"loading": false, "newTitle": "", signalItemCount: len(todos),
	}); err != nil {
		h.fail(r.Context(), w, "todo: create signals failed", err, "save failed")
		return
	}
	if err := h.patchListSelf(sse, todos, r); err != nil {
		h.fail(r.Context(), w, "todo: create patch failed", err, "save failed")
		return
	}
	if err := emitToast(sse, "Added", "success"); err != nil {
		h.fail(r.Context(), w, "todo: create toast failed", err, "save failed")
	}
}

// patchListSelf tags the originating mutation as "self" (self-origin entry
// animation) and patches the list with view transitions, merging the fresh
// item count in the same SSE response.
func (h *Handler) patchListSelf(sse *sdk.ServerSentEventGenerator, todos []todo.Todo, r *http.Request) error {
	if err := dshelpers.MergeSignals(sse, map[string]any{
		"lastItemSource": "self",
		signalItemCount:  len(todos),
	}); err != nil {
		return err
	}
	signals := h.displaySignals(r, todos, filterOf(r), scopeOf(r))
	return patchSSE(sse, h.renderListRegion(signals, h.resolveSkin(r)),
		sdk.WithSelector("#todo-list"),
		sdk.WithViewTransitions())
}

// context; disconnects surface as write errors, checked per call.
//
//nolint:contextcheck // SSE emission ends in datastar-go sinks, which take no
func (h *Handler) handleToggle(w http.ResponseWriter, r *http.Request) {
	owner, err := h.ownerID(r)
	if err != nil {
		redirectLogin(w, r)
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	// Visibility-scoped read: own rows plus public rows. A private row of
	// another user reads as "not found" — indistinguishable from missing,
	// so existence never leaks across tenants.
	row, err := h.Q.GetTodoVisible(ctx, dbgen.GetTodoVisibleParams{ID: id, Owner: owner})
	if err != nil {
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
	if serr := h.Q.SetTodoCompletedByID(ctx, dbgen.SetTodoCompletedByIDParams{
		Completed: !current.Completed, Updated: now, ID: id,
	}); serr != nil {
		h.fail(r.Context(), w, "todo: toggle save failed", fmt.Errorf("id %s: %w", id, serr), "toggle failed")
		return
	}

	// Fan-out before the replay short-circuit (see handleCreate): the
	// "replayed" acknowledgment carries no patch, so the broadcast is the
	// only path live tabs learn of the toggle through.
	h.publishSync(r, owner, current.IsShared(),
		recordEvent{Event: "toggled", ID: id, Done: !current.Completed},
		natswire.CrudOpToggle, &natswire.CrudOpData{ID: id, Completed: !current.Completed})

	if r.Header.Get("X-Offline-Replay") == "1" {
		writeText(w, http.StatusOK, "replayed")
		return
	}

	_, role := h.roleOf(r)
	todos, err := h.listVisible(ctx, owner, role, filterOf(r), scopeOf(r))
	if err != nil {
		h.fail(r.Context(), w, "todo: list after toggle failed", err, "error listing todos")
		return
	}
	sse := sdk.NewSSE(w, r)
	if err := h.patchListSelf(sse, todos, r); err != nil {
		h.fail(r.Context(), w, "todo: toggle patch failed", err, "toggle failed")
	}
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
//
//nolint:contextcheck // SSE emission ends in datastar-go sinks, which take no
func (h *Handler) handleDelete(w http.ResponseWriter, r *http.Request) {
	owner, err := h.ownerID(r)
	if err != nil {
		redirectLogin(w, r)
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	row, err := h.Q.GetTodoVisible(ctx, dbgen.GetTodoVisibleParams{ID: id, Owner: owner})
	if err != nil {
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
		writeText(w, http.StatusForbidden, "viewer role cannot delete")
		return
	}
	if derr := deleteTodoIndexed(ctx, h.Q, h.Index, h.DB, owner, item.ID); derr != nil {
		h.fail(r.Context(), w, "todo: delete failed", fmt.Errorf("id %s: %w", id, derr), "delete failed")
		return
	}

	// Fan-out before the replay short-circuit (see handleCreate): the
	// "replayed" acknowledgment carries no patch, so the broadcast is the
	// only path live tabs learn of the delete through.
	h.publishSync(r, owner, item.IsShared(),
		recordEvent{Event: "deleted", ID: item.ID},
		natswire.CrudOpDelete, &natswire.CrudOpData{ID: item.ID})

	if r.Header.Get("X-Offline-Replay") == "1" {
		writeText(w, http.StatusOK, "replayed")
		return
	}

	todos, err := h.listVisible(ctx, owner, role, filterOf(r), scopeOf(r))
	if err != nil {
		h.fail(r.Context(), w, "todo: list after delete failed", err, "error listing todos")
		return
	}
	sse := sdk.NewSSE(w, r)
	if err := dshelpers.MergeSignals(sse, map[string]any{
		"confirmingDeleteId":    "",
		"confirmingDeleteTitle": "",
	}); err != nil {
		h.fail(r.Context(), w, "todo: delete signals failed", err, "delete failed")
		return
	}
	if err := h.patchListSelf(sse, todos, r); err != nil {
		h.fail(r.Context(), w, "todo: delete patch failed", err, "delete failed")
		return
	}
	if err := emitToast(sse, fmt.Sprintf("Deleted “%s”", item.Title), "info"); err != nil {
		h.fail(r.Context(), w, "todo: delete toast failed", err, "delete failed")
	}
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
	h.publishSync(r, owner, false,
		recordEvent{Event: "cleared"},
		natswire.CrudOpClearCompleted, nil)

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
	if err := h.patchListSelf(sse, todos, r); err != nil {
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
