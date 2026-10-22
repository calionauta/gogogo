// SCOPE:feature — HTTP todo SSE stream (record-sync only)
package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/calionauta/gogogo/features/todo/components"

	"github.com/google/uuid"
	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/a-h/templ"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/todo"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/queue"
	"github.com/calionauta/gogogo/internal/rbac"
)

// handleStream opens the chi-stack SSE stream: Hub-registered, owner-scoped,
// record-sync only. Worker envelopes (retry feedback, AI suggest, workflow
// progress) travel on other channels — the chi hub carries record sync, so
// those envelopes can never arrive here misshapen.
func (h *Handler) handleStream(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("clientID")
	if clientID == "" {
		clientID = uuid.New().String()
	}
	owner, ownerErr := h.ownerID(r)
	hasOwner := ownerErr == nil
	ownerStr := ""
	if hasOwner {
		ownerStr = owner
	}

	sse := sdk.NewSSE(w, r)
	ch := make(chan []byte, config.DefaultClientQueueSize)
	h.Hub.Register(clientID, ownerStr, ch)
	defer h.Hub.UnregisterIfCurrent(clientID, ch)
	// First frame: the open acknowledgement sse-open.js waits for. Sent
	// after registration so "ack received" also means "the hub has me".
	if err := dshelpers.AckStreamOpened(sse, clientID); err != nil {
		h.fail(r.Context(), w, "todo: stream ack failed", err, "error opening stream")
		return
	}

	// The handshake re-lists the tab's OWN slice, not a blanket all/all.
	//
	// It deliberately does NOT echo SearchQuery. $searchQuery is bound two-way
	// to the search box, so it is CLIENT-owned: the stored view only advances
	// once a debounced search round-trips, so during the debounce window the
	// server's value LAGS what the user has typed. Merging it back overwrote
	// the live box with the previous value, and the debounced @get then read
	// an empty $searchQuery and listed everything — the search silently doing
	// nothing. The box is seeded on first render and patched by the search
	// handler; a reconnect has nothing to add. The TODOS still come from the
	// stored view, so a reconnect cannot re-render rows outside the slice.
	view := h.Hub.ViewOf(clientID)
	todos, err := h.handshakeTodos(r, hasOwner, owner, view)
	if err != nil {
		h.fail(r.Context(), w, "todo: list on stream open failed", err, "error listing todos")
		return
	}
	if err := mergeHandshake(sse, todo.Signals{
		Todos:            todos,
		Filter:           view.Filter,
		Scope:            view.Scope,
		ItemCount:        len(todos),
		SearchEnabled:    h.searchEnabled(),
		ConnectedClients: h.Hub.CountUserClients(),
		ClientID:         clientID,
		SidebarTab:       "queue",
		DagNatsEnabled:   h.dagnatsUIEnabled(),
		IsViewer:         h.viewerOwner(r.Context(), owner),
		IsAdmin:          h.roleOwner(r.Context(), owner) == rbac.RoleAdmin,
		UserID:           owner,
		LLMEnabled:       h.LLM != nil && h.LLM.Configured(),
		SimulatedLLM:     h.SimulatedLLM != nil && h.SimulatedLLM.Configured(),
	}, hasOwner); err != nil {
		h.fail(r.Context(), w, "todo: stream handshake failed", err, "error opening stream")
		return
	}

	flusher, canFlush := w.(http.Flusher)
	heartbeat := time.NewTicker(config.DefaultSSEHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if !canFlush {
				continue
			}
			if _, err := w.Write([]byte(": heartbeat\n\n")); err != nil {
				return
			}
			flusher.Flush()
			// The session state rides this stream too: the page does not
			// open the dedicated session stream (one long-lived connection
			// per page), so expiry is noticed here.
			hasOwner = h.revalidateSession(r, sse, hasOwner)
		case payload := <-ch:
			if err := h.dispatchEnvelope(r.Context(), sse, owner, clientID, hasOwner, payload); err != nil {
				obsArgs := append(observability.WithComponent("todo", "feature"), "error", err)
				observability.Error(r.Context(), "todo: stream dispatch failed", obsArgs...)
			}
		}
	}
}

// revalidateSession re-checks the request's cookie and patches the banner
// signal when the state changed. Split from the loop for the complexity gate;
// it runs on the heartbeat, never on a timer of its own.
// mergeHandshake sends the open frame's signals: the page state, the three
// connection-owned values todo.Signals omits from JSON, and the session flag.
//
// The session flag belongs HERE and on the heartbeat, never on a page-level
// response — a handler serving a filter or a search marshals the whole struct
// and a bool it leaves at its zero value cannot be told apart from "this tab
// really is signed out", so `sessionAuthed:false` rode every one of those and
// raised the banner on a signed-in page (docs/features.md). The same argument
// is why clientID/connectedClients/sidebarTab carry `json:"-"` and are merged
// here instead: the page seed and this handshake are their only writers.
func mergeHandshake(sse *sdk.ServerSentEventGenerator, signals todo.Signals, authed bool) error {
	if err := dshelpers.MergeSignals(sse, signals); err != nil {
		return err
	}
	if err := dshelpers.MergeSignals(sse, map[string]any{
		"clientID":         signals.ClientID,
		"connectedClients": signals.ConnectedClients,
		"sidebarTab":       signals.SidebarTab,
	}); err != nil {
		return err
	}
	return dshelpers.MergeSignals(sse, map[string]bool{"sessionAuthed": authed})
}

func (h *Handler) revalidateSession(r *http.Request, sse *sdk.ServerSentEventGenerator, authed bool) bool {
	_, err := h.Auth.Session(r)
	if (err == nil) == authed {
		return authed
	}
	authed = err == nil
	_ = dshelpers.MergeSignals(sse, map[string]bool{"sessionAuthed": authed})
	return authed
}

// applyRecordEvent re-lists scoped on a decoded record event so each stream
// renders the rows its login may see (own + shared). The listing slice
// comes from the tab's stored view prefs (clientID), so a remote mutation
// refreshes the rows without yanking the tab out of the Mine/Shared/Active
// slice it was looking at. Anonymous streams (hasOwner == false) have no
// tenant to re-list, so there is nothing to do — nil is the correct
// result, not a swallowed error.
// handshakeTodos lists the slice a freshly-opened (or reconnected) stream
// shows. Anonymous streams have no tenant to list, so they get an empty set
// rather than an error — nil would marshal as null.
func (h *Handler) handshakeTodos(
	r *http.Request, hasOwner bool, owner string, view queue.ViewPref,
) ([]todo.Todo, error) {
	if !hasOwner {
		return []todo.Todo{}, nil
	}
	todos, err := h.listVisible(r.Context(), owner, h.roleOwner(r.Context(), owner), view.Filter, view.Scope)
	if err != nil {
		return nil, err
	}
	return h.narrowBySearch(r.Context(), owner, view.Query, view.Mode, todos)
}

func (h *Handler) applyRecordEvent(
	ctx context.Context, sse *sdk.ServerSentEventGenerator, owner, clientID string, hasOwner bool, evt recordEvent,
) error {
	if evt.Event == eventWorkflowCompleted {
		return h.applyWorkflowCompleted(ctx, sse, owner, clientID, hasOwner)
	}
	if !hasOwner {
		return nil
	}
	view := h.Hub.ViewOf(clientID)
	role := h.roleOwner(ctx, owner)
	todos, err := h.listVisible(ctx, owner, role, view.Filter, view.Scope)
	if err != nil {
		return fmt.Errorf("re-list on %s: %w", evt.Event, err)
	}
	todos, err = h.narrowBySearch(ctx, owner, view.Query, view.Mode, todos)
	if err != nil {
		return fmt.Errorf("re-list search on %s: %w", evt.Event, err)
	}
	if err := dshelpers.MergeSignals(sse, map[string]any{signalItemCount: len(todos)}); err != nil {
		return err
	}
	// SearchQuery is deliberately NOT set here. It is bound two-way to the
	// search box, so it is CLIENT-owned: the stored view only advances once a
	// debounced search round-trips, so during the debounce window the server's
	// value LAGS what the user typed. Writing it from a background re-list
	// (record event, workflow completion, reconnect) would reset the box
	// mid-typing — and because Signals' field is `json:"-"`, it is never on the
	// wire at all: the input seeds the signal from its own value=, so a merge
	// simply has no key for it. The rows still come from the stored view, so the
	// slice stays correct.
	signals := todo.Signals{
		Todos: todos, Filter: view.Filter, Scope: view.Scope, ItemCount: len(todos),
		SearchEnabled:  h.searchEnabled(),
		DagNatsEnabled: h.dagnatsUIEnabled(),
		IsViewer:       rbac.IsViewer(role),
		IsAdmin:        role == rbac.RoleAdmin,
		UserID:         owner,
	}
	//nolint:contextcheck // SSE-sink boundary: patchSSE ends in datastar-go
	// sinks, which take no context; disconnects surface as write errors.
	return patchSSE(sse, components.TodoListRegion(signals),
		sdk.WithSelector("#todo-list"),
	)
}

// applyWorkflowCompleted marks the onboarding stepper done and re-lists.
// The example todos were created server-side by the DagNats worker (via
// the store, not the HTTP API), so they never crossed the request path —
// without the explicit re-list here they would not appear until a reload.
func (h *Handler) applyWorkflowCompleted(
	ctx context.Context, sse *sdk.ServerSentEventGenerator, owner, clientID string, hasOwner bool,
) error {
	if err := h.applyOnboarding(sse, 0, 0, onbStatusCompleted, "Workflow completed", true); err != nil {
		return err
	}
	if !hasOwner {
		return nil
	}
	role := h.roleOwner(ctx, owner)
	view := h.Hub.ViewOf(clientID)
	todos, err := h.listVisible(ctx, owner, role, view.Filter, view.Scope)
	if err != nil {
		return fmt.Errorf("re-list after workflow: %w", err)
	}
	todos, err = h.narrowBySearch(ctx, owner, view.Query, view.Mode, todos)
	if err != nil {
		return fmt.Errorf("re-list search after workflow: %w", err)
	}
	//nolint:contextcheck // datastar-go's sink takes no context (a
	// third-party boundary); see patchSSE.
	return renderWorkflowDone(sse, components.TodoListRegion(todo.Signals{
		Todos: todos, Filter: view.Filter, Scope: view.Scope, ItemCount: len(todos),
		SearchEnabled: h.searchEnabled(),
		IsViewer:      rbac.IsViewer(role),
		IsAdmin:       role == rbac.RoleAdmin,
		UserID:        owner,
	}))
}

// renderWorkflowDone patches the re-listed region and narrates completion.
// It is context-free by construction: the only sinks are datastar-go writes
// that take none, and the DB read that does need a context stays in the
// caller. That split is what keeps the boundary declared in one place.
func renderWorkflowDone(sse *sdk.ServerSentEventGenerator, region templ.Component) error {
	if err := dshelpers.RenderAndPatch(sse, region,
		sdk.WithSelector("#todo-list"),
	); err != nil {
		return err
	}
	return emitToast(sse, "Workflow completed — 3 example todos created", retryStatusSuccess)
}
