// SCOPE:feature — HTTP todo offline-replay propagation (red-proof).
//
// Regression guard for the offline "Add button stuck + todo lost" class:
// when the Service Worker replays a queued create, the request carries
// the originating tab's clientID (the SW replays the URL verbatim) AND
// X-Offline-Replay: 1, and its response is a bare "replayed"
// acknowledgment — no synchronous SSE patch. If the hub broadcast
// excludes that clientID, the tab that queued the mutation never learns
// the server row and its optimistic (data-pending) row sticks forever.
// Replays must therefore fan out to every stream, originator included:
// a background replay is not the originating interaction, so the
// no-echo exclusion does not apply to it.
package handlers

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestHandlersTodoReplayReachesOtherTab is the same contract for a DIFFERENT
// tab, which is the shape a user reports: "added a task offline in one browser
// and the other one never showed it". The replay's broadcast must fan out to
// every stream of the owner (not just the queueing tab), so an already-open tab
// re-lists without a refresh.
func TestHandlersTodoReplayReachesOtherTab(t *testing.T) {
	f := newHandlersFixture(t)
	srv := httptest.NewServer(f.r)
	defer srv.Close()

	const (
		email    = "replay-other@example.com"
		password = "Correct-horse-99!"
		title    = "Replayed-to-tab-B"
	)

	clientA, err := signupClient(srv.URL, email, password)
	if err != nil {
		t.Fatalf("signup: %v", err)
	}

	// Tab B is a second session of the same owner, opened BEFORE the replay.
	tokenB, _, lerr := f.auth.Login(context.Background(), email, password)
	if lerr != nil {
		t.Fatalf("second-session login: %v", lerr)
	}
	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	streamReq, err := http.NewRequestWithContext(streamCtx,
		http.MethodGet, srv.URL+"/api/todos/stream?clientID=tab-b", nil)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	streamReq.AddCookie(f.auth.SetSessionCookie(tokenB))
	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer streamResp.Body.Close()
	if streamResp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", streamResp.StatusCode)
	}

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

	replayReq, err := http.NewRequestWithContext(context.Background(),
		http.MethodPost, srv.URL+"/api/todos?filter=all&clientID=tab-a",
		strings.NewReader(url.Values{"title": {title}}.Encode()))
	if err != nil {
		t.Fatalf("replay request: %v", err)
	}
	replayReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	replayReq.Header.Set("X-Offline-Replay", "1")
	replayResp, err := clientA.Do(replayReq)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	replayResp.Body.Close()
	if replayResp.StatusCode != http.StatusOK {
		t.Fatalf("replay status = %d", replayResp.StatusCode)
	}

	select {
	case <-sawTitle:
	case <-time.After(10 * time.Second):
		t.Fatalf("tab B never saw %q after an offline replay (other tabs must refresh live)", title)
	}
}

// signupClient registers email and returns an HTTP client carrying its session
// cookie (the offline-replay tests drive the real HTTP surface this way).
func signupClient(base, email, password string) (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Jar: jar, Timeout: 15 * time.Second}
	body := strings.NewReader(`{"email":` + strconv.Quote(email) + `,"password":"` + password + `"}`)
	req, err := http.NewRequestWithContext(context.Background(),
		http.MethodPost, base+"/api/auth/signup/credential", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("signup status %d", resp.StatusCode)
	}
	return client, nil
}

// TestHandlersTodoReplayReachesOriginator opens one tab's stream and issues
// the create as an offline replay from that same tab (same clientID,
// X-Offline-Replay: 1). The stream must still carry the created title —
// the replay acknowledgment carries no patch, so the broadcast is the
// tab's only path to the server row.
func TestHandlersTodoReplayReachesOriginator(t *testing.T) {
	f := newHandlersFixture(t)
	srv := httptest.NewServer(f.r)
	defer srv.Close()

	const (
		email    = "replay-origin@example.com"
		password = "Correct-horse-99!"
		title    = "Replayed-from-tab-A"
	)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("jar: %v", err)
	}
	client := &http.Client{Jar: jar, Timeout: 15 * time.Second}
	signup := strings.NewReader(`{"email":` + strconv.Quote(email) + `,"password":"` + password + `"}`)
	signupReq, err := http.NewRequestWithContext(context.Background(),
		http.MethodPost, srv.URL+"/api/auth/signup/credential", signup)
	if err != nil {
		t.Fatalf("signup request: %v", err)
	}
	signupReq.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(signupReq)
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("signup status = %d", resp.StatusCode)
	}

	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	streamReq, err := http.NewRequestWithContext(streamCtx,
		http.MethodGet, srv.URL+"/api/todos/stream?clientID=tab-a", nil)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	streamResp, err := client.Do(streamReq)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer streamResp.Body.Close()
	if streamResp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", streamResp.StatusCode)
	}

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

	// The SW replay shape: same tab's clientID on the URL, the replay
	// header set, form body verbatim.
	create := url.Values{"title": {title}}
	createReq, err := http.NewRequestWithContext(context.Background(),
		http.MethodPost, srv.URL+"/api/todos?filter=all&clientID=tab-a",
		strings.NewReader(create.Encode()))
	if err != nil {
		t.Fatalf("replay request: %v", err)
	}
	createReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	createReq.Header.Set("X-Offline-Replay", "1")
	createResp, err := client.Do(createReq)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	createResp.Body.Close()
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("replay status = %d", createResp.StatusCode)
	}

	select {
	case <-sawTitle:
		// The originator's own stream carried the record event +
		// re-listed region despite the clientID match.
	case <-time.After(10 * time.Second):
		t.Fatalf("originator never saw %q on its stream (replay excluded the queued clientID)", title)
	}
}
