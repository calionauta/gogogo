// SCOPE:core — Service worker stream-skip contract.
package server

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/web/resources"
)

// TestServiceWorkerSkipsEverySSERoute pins the gotcha that has bitten the
// session stream: the service worker intercepts /api/*, and caching an SSE
// response never completes. The response clone keeps the stream open, the
// browser's EventSource sees an error, reconnects, and each retry adds a
// connection — after a handful of navigations the six-per-host budget is gone
// and the next page hangs on load. A new stream endpoint must be added here
// AND to SKIP_PATTERNS.
func TestServiceWorkerSkipsEverySSERoute(t *testing.T) {
	raw, err := fs.ReadFile(resources.StaticFS(), "sw.js")
	if err != nil {
		t.Fatalf("read sw.js: %v", err)
	}
	sw := string(raw)
	for _, route := range []string{
		"/api/todos/stream",
		"/api/todos/events",
		"/api/chat/stream",
		"/api/notes/",
		"/api/notifications/stream",
		"/api/session/stream",
		"/api/whiteboard/",
	} {
		if !strings.Contains(sw, `"`+route+`"`) {
			t.Errorf("sw.js SKIP_PATTERNS missing %q — the service worker will cache an SSE "+
				"response, the EventSource will retry, and every page after that can starve "+
				"on the browser's per-host connection cap", route)
		}
	}
}
