// SCOPE:feature — notifications: the shell's badge poll endpoint.
package handlers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestBadgeEndpointAnswersTheOwnerCount pins the shell's refresh affordance: the
// page that already holds a feature stream asks for one signal patch instead of
// opening a second SSE connection. The response is a Datastar signal frame,
// owner-scoped like every other read.
func TestBadgeEndpointAnswersTheOwnerCount(t *testing.T) {
	h, q, _ := newFixture(t)
	server, cookie := signedInServer(t, h, "badge@demo.app")
	u, err := q.GetUserByEmail(context.Background(), "badge@demo.app")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if derr := h.Deliver(context.Background(), Delivery{
		Owner: u.ID, TransactionID: "badge:1", Workflow: "reminder",
		Subject: "Unread", Body: "counts once",
	}); derr != nil {
		t.Fatalf("deliver: %v", derr)
	}

	req, rerr := http.NewRequestWithContext(context.Background(), http.MethodGet,
		server.URL+"/api/notifications/badge", nil)
	if rerr != nil {
		t.Fatalf("build badge request: %v", rerr)
	}
	req.AddCookie(cookie)
	resp, serr := http.DefaultClient.Do(req)
	if serr != nil {
		t.Fatalf("badge request: %v", serr)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("badge status = %d (%s)", resp.StatusCode, body)
	}
	for _, want := range []string{"datastar-patch-signals", `"inboxUnread":1`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("badge response missing %q:\n%s", want, body)
		}
	}
}
