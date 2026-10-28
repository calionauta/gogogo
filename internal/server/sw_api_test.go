// SCOPE:core — the Service Worker never touches the API.
//
// This replaced the SSE skip-list. The worker used to intercept /api/* and
// needed an explicit SKIP_PATTERNS entry per streaming route, because caching
// an SSE response never completes: the clone stays open, the EventSource sees
// an error, reconnects, and each retry adds a connection until the six-per-host
// budget is gone. Adding a stream meant remembering the list — and
// internal/server/sse_skip_test.go existed only to remember it for you.
//
// The contract is now structural: the worker handles NAVIGATIONS only, so no
// stream, cached GET or mutation can ever pass through it. A future edit that
// branches on url.pathname would fail here.
package server

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/web/resources"
)

func TestServiceWorkerNeverInterceptsAPI(t *testing.T) {
	raw, err := fs.ReadFile(resources.StaticFS(), "sw.js")
	if err != nil {
		t.Fatalf("read sw.js: %v", err)
	}
	sw := string(raw)

	// The one guard that keeps the API out: the fetch handler returns for
	// everything that is not a navigation.
	if !strings.Contains(sw, `e.request.mode !== "navigate"`) {
		t.Error("sw.js no longer gates its fetch handler on a navigation — API and SSE traffic would be intercepted")
	}
	if strings.Contains(sw, "url.pathname") {
		t.Error("sw.js branches on url.pathname; the worker must not have an API path surface")
	}
	// One interception point, and it is the page cache.
	if got := strings.Count(sw, "respondWith("); got != 1 {
		t.Errorf("sw.js respondWith() calls = %d, want exactly 1 (the navigation cache)", got)
	}
	// The whole offline-write path moved to the page outbox. None of its
	// vocabulary may come back here: a second queue is the failure mode this
	// design removed.
	for _, banned := range []string{"indexedDB.open", `addEventListener("sync"`, "gogogo-sync", "SKIP_PATTERNS"} {
		if strings.Contains(sw, banned) {
			t.Errorf("sw.js carries %q — offline writes belong to the page outbox, not the worker", banned)
		}
	}
}
