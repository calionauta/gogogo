// SCOPE:feature — HTTP todo cross-session propagation (red-proof for the realtime contract)
//
// Flat replacement for the legacy e2e that used to boot the
// real binary and drive /api/realtime (features/todo/realtime_propagation_test.go,
// which died with the framework boot: nothing served the old endpoint anymore). The
// contract it guarded is unchanged — creating a todo in one tab must
// surface in another already-open tab — and the HTTP stack answers it
// through its own channels: the owner-scoped SSE hub (record-sync) fed
// by broadcastRecord on every mutation.
package handlers

import (
	"bufio"
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestHandlersTodoCrossSessionCreatePropagates boots a real server over the
// real route composition, signs one user up from tab A, opens the todo
// SSE stream from tab B (a second session of the same owner, the exact
// shape two browser tabs have), and creates from A. B must receive the
// record event and the re-listed region carrying A's title.
func TestHandlersTodoCrossSessionCreatePropagates(t *testing.T) {
	f := newHandlersFixture(t)
	srv := httptest.NewServer(f.r)
	defer srv.Close()

	const (
		email    = "cross-tab@example.com"
		password = "Correct-horse-99!"
		title    = "From-tab-A"
	)

	// Tab A: signup over the real HTTP surface; the jar keeps the cookie.
	jarA, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("jar: %v", err)
	}
	clientA := &http.Client{Jar: jarA, Timeout: 15 * time.Second}
	signup := strings.NewReader(`{"email":` + strconv.Quote(email) + `,"password":"` + password + `"}`)
	signupReq, err := http.NewRequestWithContext(context.Background(),
		http.MethodPost, srv.URL+"/api/auth/signup/credential", signup)
	if err != nil {
		t.Fatalf("signup request: %v", err)
	}
	signupReq.Header.Set("Content-Type", "application/json")
	resp, err := clientA.Do(signupReq)
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("signup status = %d", resp.StatusCode)
	}

	// Tab B: a SECOND session of the SAME owner (what a second tab is).
	// openSession is auth-internal, so the session comes from the public
	// Login and the cookie is built by hand — same attributes SetSessionCookie
	// issues, which is what the handlers read.
	tokenB, _, lerr := f.auth.Login(context.Background(), email, password)
	if lerr != nil {
		t.Fatalf("second-session login: %v", lerr)
	}
	// The production cookie factory (Secure/httpOnly/SameSite), not a
	// hand-built one: the cookie handlers read must be the one
	// SetSessionCookie issues.
	cookieB := f.auth.SetSessionCookie(tokenB)

	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	streamReq, err := http.NewRequestWithContext(streamCtx,
		http.MethodGet, srv.URL+"/api/todos/stream?clientID=tab-b", nil)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	streamReq.AddCookie(cookieB)
	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer streamResp.Body.Close()
	if streamResp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", streamResp.StatusCode)
	}

	// Read the stream until A's title lands (or the budget runs out — the
	// hub heartbeats, so a blocked read must be bounded or the test hangs
	// instead of failing).
	sawTitle := make(chan struct{})
	go func() {
		defer close(sawTitle)
		scanner := bufio.NewScanner(streamResp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			if bytesContains(scanner.Bytes(), []byte(title)) {
				return
			}
		}
	}()

	create := url.Values{"title": {title}}
	createReq, err := http.NewRequestWithContext(context.Background(),
		http.MethodPost, srv.URL+"/api/todos?filter=all", strings.NewReader(create.Encode()))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	createReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	createResp, err := clientA.Do(createReq)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	createResp.Body.Close()
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("create status = %d", createResp.StatusCode)
	}
	select {
	case <-sawTitle:
		// B's stream carried the record event + re-listed region.
	case <-time.After(10 * time.Second):
		t.Fatalf("tab B never saw %q on its stream (record-sync broken)", title)
	}
}

func bytesContains(b, sub []byte) bool {
	for i := 0; i+len(sub) <= len(b); i++ {
		if string(b[i:i+len(sub)]) == string(sub) {
			return true
		}
	}
	return false
}
