package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/calionauta/gogogo/internal/queue"
)

func TestStreamStatusReflectsHubRegistration(t *testing.T) {
	t.Parallel()
	hub := queue.NewSSEHub()
	h := StreamStatusHandler(hub)

	get := func(clientID string) (int, string) {
		t.Helper()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/stream/status?clientID="+clientID, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}

	if code, body := get("ghost-tab"); code != http.StatusOK || body != `{"open":false}` {
		t.Fatalf("unknown clientID: got %d %q, want 200 %q", code, body, `{"open":false}`)
	}
	if code, body := get(""); code != http.StatusOK || body != `{"open":false}` {
		t.Fatalf("empty clientID: got %d %q, want 200 %q", code, body, `{"open":false}`)
	}
	ch := make(chan []byte, 1)
	hub.Register("tab-1", "user-1", ch)
	if code, body := get("tab-1"); code != http.StatusOK || body != `{"open":true}` {
		t.Fatalf("registered clientID: got %d %q, want 200 %q", code, body, `{"open":true}`)
	}
	hub.Unregister("tab-1")
	if code, body := get("tab-1"); code != http.StatusOK || body != `{"open":false}` {
		t.Fatalf("unregistered clientID: got %d %q, want 200 %q", code, body, `{"open":false}`)
	}
}
