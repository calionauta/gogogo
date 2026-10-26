// SCOPE:plugin — Datastar stream helpers contract tests.
package datastar

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	datastarsdk "github.com/starfederation/datastar-go/datastar"
)

// ackValue renders one open frame and returns the streamOpen value the
// browser would apply.
func ackValue(t *testing.T, clientID string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/stream", nil)
	if err := AckStreamOpened(datastarsdk.NewSSE(rec, req), clientID); err != nil {
		t.Fatalf("AckStreamOpened: %v", err)
	}
	for line := range strings.SplitSeq(rec.Body.String(), "\n") {
		payload, ok := strings.CutPrefix(line, "data: signals ")
		if !ok {
			continue
		}
		var signals map[string]string
		if err := json.Unmarshal([]byte(payload), &signals); err != nil {
			t.Fatalf("decode ack signals %q: %v", payload, err)
		}
		return signals[StreamOpenedSignal]
	}
	t.Fatalf("ack frame not found in %q", rec.Body.String())
	return ""
}

// TestAckStreamOpenedChangesOnEveryOpen is the guard for the reconnect bug:
// a tab reuses its clientID (sessionStorage), and Datastar dispatches
// `datastar-signal-patch` only when a value CHANGES. A bare clientID ack was
// therefore invisible on the second open, so the opener concluded the healthy
// stream never opened, re-clicked (cancelling it), and churned until failure.
// Every open must carry a distinct value.
func TestAckStreamOpenedChangesOnEveryOpen(t *testing.T) {
	t.Parallel()
	first := ackValue(t, "tab-abc")
	second := ackValue(t, "tab-abc")
	if first == second {
		t.Fatalf("ack repeated %q across two opens: a reconnect is a no-op signal patch "+
			"and the opener cannot observe it", first)
	}
	if !strings.HasPrefix(first, "tab-abc:") || !strings.HasPrefix(second, "tab-abc:") {
		t.Fatalf("ack values %q / %q must keep the clientID prefix so sse-open.js can match the tab", first, second)
	}
}
