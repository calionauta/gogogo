// SCOPE:feature — Chi todo SSE stream (record-sync only)
package chi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	sdk "github.com/starfederation/datastar-go/datastar"

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
	ownerStr := ""
	if ownerErr == nil {
		ownerStr = owner
	}

	sse := sdk.NewSSE(w, r)
	ch := make(chan []byte, config.DefaultClientQueueSize)
	h.Hub.Register(clientID, ownerStr, ch)
	defer h.Hub.UnregisterIfCurrent(clientID, ch)

	todos := []todo.Todo{}
	if ownerErr == nil {
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
			if ownerErr != nil {
				continue
			}
			if err := h.streamRecord(r.Context(), sse, owner, payload); err != nil {
				slog.Error("todo: stream dispatch failed", "error", err)
			}
		}
	}
}

// streamRecord re-lists scoped on every record event so each stream renders
// its own tenant's rows. Callers skip it for anonymous streams (empty scope,
// never an unscoped list), so owner is always valid here.
// context; disconnects surface as write errors, checked per call.
//
//nolint:contextcheck // SSE emission ends in datastar-go sinks, which take no
func (h *Handler) streamRecord(ctx context.Context, sse *sdk.ServerSentEventGenerator, owner string, payload []byte) error { //nolint:lll // signature mirrors the dispatch call.
	var evt recordEvent
	if err := json.Unmarshal(payload, &evt); err != nil {
		return fmt.Errorf("decode todo event: %w", err)
	}
	todos, err := h.listOwner(ctx, owner, "all")
	if err != nil {
		return fmt.Errorf("re-list on %s: %w", evt.Event, err)
	}
	if err := dshelpers.MergeSignals(sse, map[string]any{signalItemCount: len(todos)}); err != nil {
		return err
	}
	signals := todo.Signals{Todos: todos, Filter: "all", ItemCount: len(todos)}
	return patchSSE(sse, h.renderListRegion(signals, "daisyui"),
		sdk.WithSelector("#todo-list"),
	)
}
