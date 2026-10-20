// SCOPE:feature — todo record-event stream for non-browser clients
package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/todo"
)

// eventsBuffer is the per-client hand-off queue. The bus delivers
// synchronously on the mutation goroutine, so a slow HTTP client must never
// be able to stall a write: a full queue drops the event (the client re-lists
// on the next one, and its own connect/refresh is the backstop).
const eventsBuffer = 32

// handleEvents streams record events as plain JSON (one `data:` frame per
// event) for clients that are not browsers: the native window, a CLI, an
// integration test. The web streams carry Datastar DOM patches, which is the
// wrong contract for a renderer that has no DOM.
//
// Scope is the same as the hub fan-out: a private event only reaches its
// owner, a shared event reaches every signed-in listener. No replay: the
// client lints once on connect and re-lists on each event, so a dropped frame
// is healed by the next one — the same "whole snapshot" rule the browser
// streams follow.
func (h *Handler) handleEvents(w http.ResponseWriter, r *http.Request) {
	owner, err := h.ownerID(r)
	if err != nil {
		// A stream cannot follow a login redirect usefully: the client parses
		// frames, not HTML. Fail with a status it can act on.
		writeText(w, http.StatusUnauthorized, "sign in to watch todo events")
		return
	}
	flusher, canFlush := w.(http.Flusher)
	if !canFlush {
		writeText(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	// Subscribe BEFORE the first byte: a client that reacts to ": connected"
	// by mutating would otherwise race the subscription and miss its own
	// event (the endpoint test pins the ordering).
	events := make(chan recordEvent, eventsBuffer)
	cancel := h.Bus().Subscribe(func(ev todo.Event) {
		if ev.Owner != owner && ev.Visibility != todo.VisibilityPublic {
			return // another user's private row: never leave this process
		}
		select {
		case events <- recordFromEvent(ev):
		default: // slow client: drop, the next event heals it
		}
	})
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(config.DefaultSSEHeartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev := <-events:
			payload, merr := json.Marshal(ev)
			if merr != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
