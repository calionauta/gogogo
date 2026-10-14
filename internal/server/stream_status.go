// SCOPE:core — stream liveness probe for verified SSE open.
package server

import (
	"net/http"

	"github.com/calionauta/gogogo/internal/queue"
)

// StreamStatusHandler reports whether clientID currently holds a live
// registration in the shared SSE hub. The browser stream opener clicks a
// Datastar @get it cannot observe, so it polls here until the hub confirms
// the tab registered — resolving on open, failing loudly past its bound
// instead of leaving a silent-dead stream (see
// web/resources/static/sse-open.js). No auth: clientIDs are opaque per-tab
// randoms, unguessable, and a boolean leaks nothing without one.
func StreamStatusHandler(hub *queue.SSEHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("clientID")
		open := id != "" && hub.IsRegistered(id)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if open {
			_, _ = w.Write([]byte(`{"open":true}`))
		} else {
			_, _ = w.Write([]byte(`{"open":false}`))
		}
	}
}
