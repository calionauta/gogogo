package analytics

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/events"
)

func TestPostHogCapturePostsTheProjectEvent(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/capture/" {
			t.Errorf("path = %q, want /capture/", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	posthog := &PostHog{Host: server.URL, Key: "phc_test"}
	posthog.Capture(context.Background(), events.Event{
		Name:   "ui.nav.todo",
		At:     time.Now(),
		Fields: map[string]any{"actor": "user-123", "path": "/todo"},
	})

	if got["api_key"] != "phc_test" {
		t.Errorf("api_key = %v, want the project key", got["api_key"])
	}
	if got["event"] != "ui.nav.todo" {
		t.Errorf("event = %v, want the bus event name", got["event"])
	}
	if got["distinct_id"] != "user-123" {
		t.Errorf("distinct_id = %v, want the event actor", got["distinct_id"])
	}
}

func TestFromConfigFallsBackToNoop(t *testing.T) {
	cases := []struct {
		provider string
		key      string
		enabled  bool
	}{
		{"posthog", "phc_test", true},
		{"posthog", "", false}, // a provider without a key must not send
		{"none", "phc_test", false},
		{"", "", false},
		{"unknown", "phc_test", false}, // a typo never reaches the network
	}
	for _, tc := range cases {
		if got := Enabled(FromConfig(tc.provider, tc.key, "")); got != tc.enabled {
			t.Errorf("Enabled(FromConfig(%q, key=%v)) = %v, want %v",
				tc.provider, tc.key != "", got, tc.enabled)
		}
	}
}
