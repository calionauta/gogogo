// SCOPE:feature — Chi todo mutations (create/toggle/delete/clear)
package chi

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
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/natswire"
)

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
		h.fail(w, "todo: dedupe lookup failed", derr, "save failed")
		return
	default:
		now := db.Now()
		newID := db.NewID()
		if _, cerr := h.Q.CreateTodo(ctx, dbgen.CreateTodoParams{
			ID: newID, Owner: owner, Title: title,
			Completed: false, IdemKey: idemKey, Created: now, Updated: now,
		}); cerr != nil {
			// A concurrent create may win the race on the partial unique
			// index: re-read instead of 500ing a row that now exists.
			if existing, rerr := h.Q.GetTodoByIdemKey(ctx, dbgen.GetTodoByIdemKeyParams{
				Owner: owner, IdemKey: idemKey,
			}); rerr == nil {
				createdID = existing.ID
			} else {
				h.fail(w, "todo: save failed", cerr, "save failed")
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

	if r.Header.Get("X-Offline-Replay") == "1" {
		writeText(w, http.StatusOK, "replayed")
		return
	}

	todos, err := h.listOwner(ctx, owner, filterOf(r))
	if err != nil {
		h.fail(w, "todo: list after create failed", err, "error listing todos")
		return
	}
	h.publishSync(r, owner,
		recordEvent{Event: "created", ID: createdID, Title: title},
		natswire.CrudOpCreate, &natswire.CrudOpData{ID: createdID, Title: title})
	sse := sdk.NewSSE(w, r)
	if err := dshelpers.MergeSignals(sse, map[string]any{
		"loading": false, "newTitle": "", signalItemCount: len(todos),
	}); err != nil {
		h.fail(w, "todo: create signals failed", err, "save failed")
		return
	}
	if err := h.patchListSelf(sse, todos, h.resolveSkin(r)); err != nil {
		h.fail(w, "todo: create patch failed", err, "save failed")
		return
	}
	if err := emitToast(sse, "Added", "success"); err != nil {
		h.fail(w, "todo: create toast failed", err, "save failed")
	}
}

// patchListSelf tags the originating mutation as "self" (self-origin entry
// animation) and patches the list with view transitions, merging the fresh
// item count in the same SSE response.
func (h *Handler) patchListSelf(sse *sdk.ServerSentEventGenerator, todos []todo.Todo, skin string) error {
	if err := dshelpers.MergeSignals(sse, map[string]any{
		"lastItemSource": "self",
		signalItemCount:  len(todos),
	}); err != nil {
		return err
	}
	signals := todo.Signals{
		Todos: todos, Filter: "all", ItemCount: len(todos),
		DagNatsEnabled: h.dagnatsUIEnabled(),
	}
	return patchSSE(sse, h.renderListRegion(signals, skin),
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
	current, err := h.Q.GetTodo(ctx, dbgen.GetTodoParams{ID: id, Owner: owner})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeText(w, http.StatusNotFound, "not found")
			return
		}
		h.fail(w, "todo: toggle read failed", fmt.Errorf("id %s: %w", id, err), "toggle failed")
		return
	}
	now := db.Now()
	if serr := h.Q.SetTodoCompleted(ctx, dbgen.SetTodoCompletedParams{
		Completed: !current.Completed, Updated: now, ID: id, Owner: owner,
	}); serr != nil {
		h.fail(w, "todo: toggle save failed", fmt.Errorf("id %s: %w", id, serr), "toggle failed")
		return
	}

	if r.Header.Get("X-Offline-Replay") == "1" {
		writeText(w, http.StatusOK, "replayed")
		return
	}

	todos, err := h.listOwner(ctx, owner, filterOf(r))
	if err != nil {
		h.fail(w, "todo: list after toggle failed", err, "error listing todos")
		return
	}
	h.publishSync(r, owner,
		recordEvent{Event: "toggled", ID: id, Done: !current.Completed},
		natswire.CrudOpToggle, &natswire.CrudOpData{ID: id, Completed: !current.Completed})
	sse := sdk.NewSSE(w, r)
	if err := h.patchListSelf(sse, todos, h.resolveSkin(r)); err != nil {
		h.fail(w, "todo: toggle patch failed", err, "toggle failed")
	}
}

func (h *Handler) handleConfirmDelete(w http.ResponseWriter, r *http.Request) {
	owner, err := h.ownerID(r)
	if err != nil {
		redirectLogin(w, r)
		return
	}
	id := chi.URLParam(r, "id")
	item, err := h.Q.GetTodo(r.Context(), dbgen.GetTodoParams{ID: id, Owner: owner})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeText(w, http.StatusNotFound, "not found")
			return
		}
		h.fail(w, "todo: confirm-delete read failed", err, "delete failed")
		return
	}
	sse := sdk.NewSSE(w, r)
	if err := dshelpers.MergeSignals(sse, map[string]any{
		"confirmingDeleteId":    item.ID,
		"confirmingDeleteTitle": item.Title,
	}); err != nil {
		h.fail(w, "todo: confirm-delete signals failed", err, "delete failed")
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
	item, err := h.Q.GetTodo(ctx, dbgen.GetTodoParams{ID: id, Owner: owner})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeText(w, http.StatusNotFound, "not found")
			return
		}
		h.fail(w, "todo: delete read failed", fmt.Errorf("id %s: %w", id, err), "delete failed")
		return
	}
	if derr := h.Q.DeleteTodo(ctx, dbgen.DeleteTodoParams{ID: id, Owner: owner}); derr != nil {
		h.fail(w, "todo: delete failed", fmt.Errorf("id %s: %w", id, derr), "delete failed")
		return
	}

	if r.Header.Get("X-Offline-Replay") == "1" {
		writeText(w, http.StatusOK, "replayed")
		return
	}

	todos, err := h.listOwner(ctx, owner, filterOf(r))
	if err != nil {
		h.fail(w, "todo: list after delete failed", err, "error listing todos")
		return
	}
	h.publishSync(r, owner,
		recordEvent{Event: "deleted", ID: item.ID},
		natswire.CrudOpDelete, &natswire.CrudOpData{ID: item.ID})
	sse := sdk.NewSSE(w, r)
	if err := dshelpers.MergeSignals(sse, map[string]any{
		"confirmingDeleteId":    "",
		"confirmingDeleteTitle": "",
	}); err != nil {
		h.fail(w, "todo: delete signals failed", err, "delete failed")
		return
	}
	if err := h.patchListSelf(sse, todos, h.resolveSkin(r)); err != nil {
		h.fail(w, "todo: delete patch failed", err, "delete failed")
		return
	}
	if err := emitToast(sse, fmt.Sprintf("Deleted “%s”", item.Title), "info"); err != nil {
		h.fail(w, "todo: delete toast failed", err, "delete failed")
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
	count, err := h.Q.DeleteCompletedByOwner(ctx, owner)
	if err != nil {
		h.fail(w, "todo: clear completed failed", err, "clear failed")
		return
	}

	if r.Header.Get("X-Offline-Replay") == "1" {
		writeText(w, http.StatusOK, "replayed")
		return
	}

	todos, err := h.listOwner(ctx, owner, filterOf(r))
	if err != nil {
		h.fail(w, "todo: list after clear failed", err, "error listing todos")
		return
	}
	h.publishSync(r, owner,
		recordEvent{Event: "cleared"},
		natswire.CrudOpClearCompleted, nil)
	sse := sdk.NewSSE(w, r)
	if err := h.patchListSelf(sse, todos, h.resolveSkin(r)); err != nil {
		h.fail(w, "todo: clear patch failed", err, "clear failed")
		return
	}
	if count == 0 {
		if err := emitToast(sse, "Nothing to clear", "info"); err != nil {
			h.fail(w, "todo: clear toast failed", err, "clear failed")
		}
		return
	}
	if err := emitToast(sse, fmt.Sprintf("Cleared %d completed", count), "success"); err != nil {
		h.fail(w, "todo: clear toast failed", err, "clear failed")
	}
}
