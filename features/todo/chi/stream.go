// SCOPE:feature — Chi todo SSE stream (record-sync only)
package chi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/a-h/templ"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/todo"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
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

	todos := []todo.Todo{}
	if hasOwner {
		var err error
		todos, err = h.listOwner(r.Context(), owner, "all")
		if err != nil {
			h.fail(w, "todo: list on stream open failed", err, "error listing todos")
			return
		}
	}
	if err := dshelpers.MergeSignals(sse, todo.Signals{
		Todos:            todos,
		Filter:           "all",
		ItemCount:        len(todos),
		ConnectedClients: h.Hub.Stats().Clients,
		ClientID:         clientID,
		SidebarTab:       "queue",
	}); err != nil {
		h.fail(w, "todo: stream handshake failed", err, "error opening stream")
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
		case payload := <-ch:
			if err := h.dispatchEnvelope(r.Context(), sse, owner, hasOwner, payload); err != nil {
				slog.Error("todo: stream dispatch failed", "error", err)
			}
		}
	}
}

// applyRecordEvent re-lists scoped on a decoded record event so each stream
// renders its own tenant's rows. Anonymous streams (hasOwner == false)
// have no tenant to re-list, so there is nothing to do — nil is the
// correct result, not a swallowed error.
func (h *Handler) applyRecordEvent(
	ctx context.Context, sse *sdk.ServerSentEventGenerator, owner string, hasOwner bool, evt recordEvent,
) error {
	if evt.Event == eventWorkflowCompleted {
		return h.applyWorkflowCompleted(ctx, sse, owner, hasOwner)
	}
	if !hasOwner {
		return nil
	}
	todos, err := h.listOwner(ctx, owner, "all")
	if err != nil {
		return fmt.Errorf("re-list on %s: %w", evt.Event, err)
	}
	if err := dshelpers.MergeSignals(sse, map[string]any{signalItemCount: len(todos)}); err != nil {
		return err
	}
	signals := todo.Signals{Todos: todos, Filter: "all", ItemCount: len(todos)}
	//nolint:contextcheck // SSE-sink boundary: patchSSE ends in datastar-go
	// sinks, which take no context; disconnects surface as write errors.
	return patchSSE(sse, h.renderListRegion(signals, "daisyui"),
		sdk.WithSelector("#todo-list"),
	)
}

// applyWorkflowCompleted marks the onboarding stepper done and re-lists.
// The example todos were created server-side by the DagNats worker (via
// the store, not the HTTP API), so they never crossed the request path —
// without the explicit re-list here they would not appear until a reload.
func (h *Handler) applyWorkflowCompleted(
	ctx context.Context, sse *sdk.ServerSentEventGenerator, owner string, hasOwner bool,
) error {
	if err := h.applyOnboarding(sse, 0, 0, onbStatusCompleted, "Workflow completed", true); err != nil {
		return err
	}
	if !hasOwner {
		return nil
	}
	todos, err := h.listOwner(ctx, owner, "all")
	if err != nil {
		return fmt.Errorf("re-list after workflow: %w", err)
	}
	//nolint:contextcheck // datastar-go's sink takes no context (a
	// third-party boundary); see patchSSE.
	return renderWorkflowDone(sse, h.renderListRegion(todo.Signals{
		Todos: todos, Filter: "all", ItemCount: len(todos),
	}, "daisyui"))
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
