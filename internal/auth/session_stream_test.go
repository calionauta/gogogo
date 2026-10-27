// SCOPE:core — Session stream contract tests (the banner without polling).
//
// The banner holds ONE stream open and hears only about changes, so these
// tests pin the three promises the client relies on:
//
//  1. An anonymous request gets a single authed:false frame and the
//     connection ends (no socket held for a session that does not exist).
//  2. A live session gets authed:true immediately.
//  3. A session revoked while the stream is open is reported as
//     authed:false and the stream closes, so the client can stop its
//     EventSource instead of retrying forever.
package auth

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// sessionFrame is the decoded banner frame. Unread is present only when the
// shell feed is wired (the composition root does; package tests may not).
type sessionFrame struct {
	Type   string `json:"type"`
	Authed bool   `json:"authed"`
	Unread *int   `json:"unread,omitempty"`
}

// openSessionStream starts a stream request and returns a scanner over the
// body plus a closer. The request carries token when non-empty.
func openSessionStream(t *testing.T, srv *httptest.Server, a *Auth, token string) (*bufio.Scanner, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/session/stream", nil)
	if err != nil {
		cancel()
		t.Fatalf("stream request: %v", err)
	}
	if token != "" {
		req.AddCookie(a.SetSessionCookie(token))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("stream open: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		t.Fatalf("stream status = %d, want 200", resp.StatusCode)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	return sc, func() { resp.Body.Close(); cancel() }
}

// nextFrame reads up to the next `data:` line and decodes it. Comments
// (": heartbeat") and blank lines are skipped; io.EOF surfaces as ok=false.
func nextFrame(t *testing.T, sc *bufio.Scanner) (sessionFrame, bool) {
	t.Helper()
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var f sessionFrame
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &f); err != nil {
			t.Fatalf("decode frame %q: %v", line, err)
		}
		return f, true
	}
	if err := sc.Err(); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("read stream: %v", err)
	}
	return sessionFrame{}, false
}

// TestSessionStreamAnonymous: no cookie, one false frame, then the server
// ends the response (an anonymous tab has nothing to watch).
func TestSessionStreamAnonymous(t *testing.T) {
	a := newSessionAuth(t)
	srv := httptest.NewServer(a.SessionStreamHandler())
	defer srv.Close()

	sc, closeStream := openSessionStream(t, srv, a, "")
	defer closeStream()

	f, ok := nextFrame(t, sc)
	if !ok {
		t.Fatal("anonymous stream sent no frame")
	}
	if f.Type != "session" || f.Authed {
		t.Fatalf("anonymous frame = %+v, want {session false}", f)
	}
	if _, ok := nextFrame(t, sc); ok {
		t.Fatal("anonymous stream stayed open after its one frame")
	}
}

// TestSessionStreamLiveThenRevoked: authed:true on connect, authed:false —
// and a closed stream — once the session is logged out mid-connection.
//
// RED-PROOFED: drop the validate.C branch and this hangs until the test
// timeout; send the false frame without returning and the stream never
// closes (the client would then retry forever).
func TestSessionStreamLiveThenRevoked(t *testing.T) {
	a := newSessionAuth(t)
	a.sessionRecheck = 20 * time.Millisecond
	ctx := context.Background()
	_, token, err := a.Signup(ctx, "stream@example.com", "Correct-horse-99!", "Stream")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}

	srv := httptest.NewServer(a.SessionStreamHandler())
	defer srv.Close()

	sc, closeStream := openSessionStream(t, srv, a, token)
	defer closeStream()

	f, ok := nextFrame(t, sc)
	if !ok {
		t.Fatal("live stream sent no frame")
	}
	if f.Type != "session" || !f.Authed {
		t.Fatalf("live frame = %+v, want {session true}", f)
	}

	a.Logout(ctx, token)

	deadline := time.Now().Add(10 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("revoked session was never reported on the open stream")
		}
		f, ok = nextFrame(t, sc)
		if !ok {
			t.Fatal("stream closed before reporting authed:false")
		}
		if !f.Authed {
			break
		}
	}
	if _, ok := nextFrame(t, sc); ok {
		t.Fatal("stream stayed open after reporting authed:false")
	}
}

// fakeStreamExtras is a SessionStreamExtras with a manual trigger, so a test
// can prove the stream pushes a change instead of waiting for the recheck.
type fakeStreamExtras struct {
	mu       sync.Mutex
	unread   int
	onChange func()
}

func (f *fakeStreamExtras) Unread(context.Context, string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.unread, nil
}

func (f *fakeStreamExtras) Subscribe(_ string, onChange func()) func() {
	f.mu.Lock()
	f.onChange = onChange
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		f.onChange = nil
		f.mu.Unlock()
	}
}

func (f *fakeStreamExtras) set(n int) {
	f.mu.Lock()
	f.unread = n
	fn := f.onChange
	f.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// scanFrames decodes a session stream in the background, so a test can bound
// each wait instead of blocking inside Scanner.Scan (a blocking read makes a
// missing push look like a slow test until the 60s recheck saves it).
func scanFrames(sc *bufio.Scanner) <-chan sessionFrame {
	frames := make(chan sessionFrame, 8)
	go func() {
		defer close(frames)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var f sessionFrame
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &f); err != nil {
				continue
			}
			frames <- f
		}
	}()
	return frames
}

// awaitFrame reads one frame or fails when the stream stays silent. The
// timeout is the assertion: the recheck ticker is pushed out of reach in the
// test, so only the feed's wake can answer.
func awaitFrame(t *testing.T, frames <-chan sessionFrame, what string) sessionFrame {
	t.Helper()
	select {
	case f, ok := <-frames:
		if !ok {
			t.Fatalf("stream closed before %s", what)
		}
		return f
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s on the session stream", what)
	}
	return sessionFrame{}
}

// TestSessionStreamCarriesUnread proves the leader stream is also the inbox
// badge's wire: the first frame carries the current count, and a feed change
// pushes a new frame without a second connection or a client request.
//
// RED-PROOFED: drop the wake case (or the unread field) and this fails in 3s
// instead of waiting for the 60s recheck, because the recheck is parked at an
// hour below.
func TestSessionStreamCarriesUnread(t *testing.T) {
	a := newSessionAuth(t)
	feed := &fakeStreamExtras{unread: 1}
	a.SetSessionStreamExtras(feed)
	a.sessionRecheck = time.Hour // only the wake can deliver the change
	ctx := context.Background()
	_, token, err := a.Signup(ctx, "unread@example.com", "Correct-horse-99!", "Unread")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}

	srv := httptest.NewServer(a.SessionStreamHandler())
	defer srv.Close()

	sc, closeStream := openSessionStream(t, srv, a, token)
	defer closeStream()
	frames := scanFrames(sc)

	first := awaitFrame(t, frames, "the opening frame")
	if first.Type != "session" || !first.Authed {
		t.Fatalf("first frame = %+v, want {session true}", first)
	}
	if first.Unread == nil || *first.Unread != 1 {
		t.Fatalf("first unread = %v, want 1", first.Unread)
	}

	feed.set(3)
	changed := awaitFrame(t, frames, "the unread change")
	if changed.Unread == nil || *changed.Unread != 3 {
		t.Fatalf("changed unread = %v, want 3", changed.Unread)
	}
}
