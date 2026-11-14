// Capability writes are domain events.
//
// Reads are the access log's job (one line per capability request); the event
// bus carries the WRITE — the fact a hook or an analytics provider reacts to.
// Emitting every request would turn a webhook into a firehose and an analytics
// project into a bill, so the bus stays on the mutating half.
package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/calionauta/gogogo/internal/events"
)

const (
	// eventCapabilityInteraction names a mutating request a capability served.
	eventCapabilityInteraction = "capability.interaction"
	// fieldPath is the structured-log/event key for a request path. Shared so
	// the access log line and the event read the same word.
	fieldPath = "path"
)

// emitInteraction publishes one event after a mutating capability request. The
// send is non-blocking and nil-safe: a tree with no bus pays nothing.
func emitInteraction(bus *events.Bus, capability string, next http.Handler) http.Handler {
	if bus == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !mutating(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		bus.Emit(r.Context(), events.Event{
			Name: eventCapabilityInteraction,
			Fields: map[string]any{
				"capability":  capability,
				"method":      r.Method,
				fieldPath:     r.URL.Path,
				"status":      interactionStatus(ww.Status()),
				"duration_ms": time.Since(start).Milliseconds(),
			},
		})
	})
}

// mutating reports whether a method changes server state. GET/HEAD/OPTIONS are
// the read half, and a HEAD rides GET in every router here.
func mutating(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

// interactionStatus turns a writer that never wrote into the status the client
// observed (200), so an event never carries status 0.
func interactionStatus(status int) int {
	if status == 0 {
		return http.StatusOK
	}
	return status
}
