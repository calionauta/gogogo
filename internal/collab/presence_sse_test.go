package collab

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	natsio "github.com/nats-io/nats.go"

	"github.com/calionauta/gogogo/internal/nats"
)

// TestPresence_SSEBridgeE2E is the browser-side e2e guard for the central
// presence bridge: it drives collab.PresenceSSEHandler (the exact handler
// router/collab_jetstream.go registers at GET /api/collab/presence/{docID})
// over httptest, then publishes a cursor on app.presence.<docID> (as a
// desktop edge would). The SSE response must carry the cursor event to the
// browser client — proving the web UI will see live edge cursors.
func TestPresence_SSEBridgeE2E(t *testing.T) {
	h, err := nats.StartEmbedded(t.TempDir())
	if err != nil {
		t.Fatalf("nats start: %v", err)
	}
	t.Cleanup(h.Close)
	nc, err := natsio.Connect(nats.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	defer nc.Close()

	// Register on a real mux so r.PathValue("docID") resolves, mirroring
	// the production route registration.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/collab/presence/{docID}", PresenceSSEHandler(nc))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := t.Context()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/collab/presence/sse-doc", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// Edge publishes a cursor on the same subject.
	go func() {
		time.Sleep(200 * time.Millisecond)
		msg := PresenceMsg{Type: "cursor", Doc: "sse-doc", User: "edge", X: 0.3, Y: 0.7, TS: time.Now().UnixMilli()}
		data, _ := json.Marshal(msg)
		if pubErr := nc.Publish(PresenceSubject("sse-doc"), data); pubErr != nil {
			t.Logf("publish: %v", pubErr)
		} else {
			t.Logf("published edge cursor to %s", PresenceSubject("sse-doc"))
		}
	}()

	// Read the first SSE event with a hard deadline. A single goroutine
	// drains resp.Body; the main loop waits on it (no per-iteration
	// respawn, which would leak blocked Scan calls and hang).
	type readResult struct{ line string }
	ch := make(chan readResult, 1)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				ch <- readResult{line: line}
				return
			}
		}
		ch <- readResult{line: ""}
	}()

	select {
	case res := <-ch:
		got := res.line
		if got == "" {
			t.Fatal("SSE stream closed with no event")
		}
		if !strings.Contains(got, `"user":"edge"`) || !strings.Contains(got, `"x":0.3`) {
			t.Fatalf("SSE event missing edge cursor: %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no SSE event received from bridge within timeout")
	}
}
