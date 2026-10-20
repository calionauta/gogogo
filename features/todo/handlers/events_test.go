// SCOPE:feature — JSON event stream contract (non-browser clients).
package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/features/todo"
)

// openEvents connects the JSON event stream and decodes its frames into a
// channel. A frame is one `data:` line; comments (heartbeats) are skipped.
func openEvents(t *testing.T, srv *httptest.Server, cookie *http.Cookie) (<-chan recordEvent, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/todos/events", nil)
	if err != nil {
		cancel()
		t.Fatalf("events request: %v", err)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("events open: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		t.Fatalf("events status = %d, want 200", resp.StatusCode)
	}
	out := make(chan recordEvent, 16)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 4096), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var evt recordEvent
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &evt); err != nil {
				continue
			}
			select {
			case out <- evt:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, func() { resp.Body.Close(); cancel() }
}

func waitEvent(t *testing.T, events <-chan recordEvent, id string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case evt, ok := <-events:
			if !ok {
				t.Fatalf("stream closed before event %q", id)
			}
			if evt.ID == id {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for event %q", id)
		}
	}
}

func waitNoEvent(t *testing.T, events <-chan recordEvent) {
	t.Helper()
	select {
	case evt := <-events:
		t.Fatalf("unexpected event %+v", evt)
	case <-time.After(250 * time.Millisecond):
	}
}

// TestHandlersTodoEventsScopesByOwner pins the non-browser stream: a private
// event reaches only its owner, a shared one reaches every listener, and the
// frame is plain JSON (no Datastar).
//
// RED-PROOFED: drop the owner/visibility filter and the "other private" event
// arrives (a cross-user leak); drop the Bus subscription and nothing arrives.
func TestHandlersTodoEventsScopesByOwner(t *testing.T) {
	f := newHandlersFixture(t)
	cookie := f.signup("events-a@example.com")
	owner := f.ownerOf(t, cookie)
	srv := httptest.NewServer(f.r)
	defer srv.Close()

	events, closeStream := openEvents(t, srv, cookie)
	defer closeStream()

	// Another user's private row must never leave the process.
	f.h.Bus().Publish(context.Background(), todo.Event{
		Kind: todo.Created, ID: "other-private", Owner: "someone-else", Visibility: todo.VisibilityPrivate,
	})
	waitNoEvent(t, events)

	// Own row: delivered with the same record-event shape the browser uses.
	f.h.Bus().Publish(context.Background(), todo.Event{
		Kind: todo.Created, ID: "mine", Title: "my row", Owner: owner, Visibility: todo.VisibilityPrivate,
	})
	waitEvent(t, events, "mine")

	// Shared row: every signed-in listener hears it.
	f.h.Bus().Publish(context.Background(), todo.Event{
		Kind: todo.Created, ID: "shared", Owner: "someone-else", Visibility: todo.VisibilityPublic,
	})
	waitEvent(t, events, "shared")
}

// TestHandlersTodoEventsRequiresSession: no cookie means 401, not a redirect
// (a stream client parses frames, not HTML).
func TestHandlersTodoEventsRequiresSession(t *testing.T) {
	f := newHandlersFixture(t)
	srv := httptest.NewServer(f.r)
	defer srv.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/api/todos/events", nil)
	if err != nil {
		t.Fatalf("events request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("events request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous events status = %d, want 401", resp.StatusCode)
	}
}
