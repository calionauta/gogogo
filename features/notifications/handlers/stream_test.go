// SCOPE:feature — notifications: the live inbox stream, end to end.
package handlers

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/auth"
)

// TestStreamPushesADeliveryToTheOpenTab proves the whole live path: a signed-in
// tab opens the stream, a delivery written by the worker reaches the bus, and
// the tab receives the inbox patch with the new row — no reload, no polling.
func TestStreamPushesADeliveryToTheOpenTab(t *testing.T) {
	h, q, _ := newFixture(t)

	// Sign up over the real auth routes so the stream carries a real session
	// cookie, not a fabricated one.
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", h.auth.Routes())
	mux.Handle("/", h.Routes())
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	body := `{"email":"stream@demo.app","password":"correct-horse-battery-staple"}`
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		server.URL+"/api/auth/signup/credential", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build signup: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("signup status = %d", resp.StatusCode)
	}
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("signup returned no session cookie")
	}
	u, err := q.GetUserByEmail(context.Background(), "stream@demo.app")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	// Open the stream. The body stays open until cleanup; closing it is what
	// unblocks the reader goroutine.
	streamReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		server.URL+"/api/notifications/stream?clientID=tab-test", nil)
	if err != nil {
		t.Fatalf("build stream: %v", err)
	}
	streamReq.AddCookie(cookie)
	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	t.Cleanup(func() { _ = streamResp.Body.Close() })
	if streamResp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", streamResp.StatusCode)
	}

	lines := make(chan string, 128)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(streamResp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()

	waitFor := func(want string) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case line, ok := <-lines:
				if !ok {
					t.Fatalf("stream closed before %q", want)
				}
				if strings.Contains(line, want) {
					return
				}
			case <-deadline:
				t.Fatalf("timed out waiting for %q on the stream", want)
			}
		}
	}

	// Handshake first: the ack and the initial inbox frame.
	waitFor("streamOpen")
	waitFor("Nothing here yet.")

	if err := h.Deliver(context.Background(), Delivery{
		Owner:         u.ID,
		TransactionID: "stream:1",
		Workflow:      "reminder",
		Subject:       "Live delivery",
		Body:          "This row arrived over the stream.",
	}); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	waitFor("Live delivery")
}

// TestStreamRefusesAnonymous pins the handler's own guard: the stream never
// opens without a session, even when RBAC is off.
func TestStreamRefusesAnonymous(t *testing.T) {
	h, _, _ := newFixture(t)
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/notifications/stream", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous stream = %d, want 401", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "datastar") {
		t.Fatalf("anonymous stream leaked an SSE frame: %s", rec.Body.String())
	}
}
