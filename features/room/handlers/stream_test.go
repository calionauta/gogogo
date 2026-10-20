// SCOPE:feature — Room SSE stream contract tests (presence by connection).
//
// The room stopped polling: a tab opens ONE stream, the grain learns about
// the connection on connect and about its death on disconnect, and every
// roster change is pushed to the tabs watching that room. These tests pin
// the three things that make that safe:
//
//  1. A connect joins and the tab receives the current card immediately (no
//     poll interval to wait out) — so the page can paint from the stream.
//  2. A disconnect leaves, and peers see a roster WITHOUT that member.
//  3. Two tabs of one person: closing one keeps the member (the refcount in
//     the grain), which is the failure mode an SSE leave signal introduces
//     and a heartbeat could not have.
//
// The pushed frame is a Datastar DOM patch, not JSON, so "what the roster
// says" has to be read out of the rendered markup. That is intentional: it
// asserts on what a browser would actually show.
package handlers

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/queue"
)

// roomHostServer boots the REAL GoAkt host behind a REAL HTTP server.
// A recorder cannot carry an SSE stream (it has no Flusher and no streaming
// body), so these tests need the same shape as the notes presence suite.
func roomHostServer(t *testing.T) (*httptest.Server, func(string) *http.Cookie, *queue.SSEHub) {
	t.Helper()
	mux, signup, hub := roomHostFixtureHub(t)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, signup, hub
}

// waitUnregistered blocks until the hub no longer holds clientID. The SSE
// teardown is asynchronous on purpose (the write loop notices the dead
// socket on its next write), so a test that needs "this tab is really gone"
// has to wait for the hub, not for its own closed body.
func waitUnregistered(ctx context.Context, hub *queue.SSEHub, clientID string) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !hub.IsRegistered(clientID) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// rosterEvent is the roster recovered from one pushed Datastar frame.
type rosterEvent struct {
	Presenter  string
	Generation int
	Names      []string
	Raw        string
}

func (s rosterEvent) has(name string) bool {
	return slices.Contains(s.Names, name)
}

// parseRosterFrame reads the roster out of one patch frame: the presenter
// cell's text and the member rows. Absent rows mean an empty roster (the
// renderer emits a placeholder line for that case, which is skipped).
func parseRosterFrame(frame string) rosterEvent {
	gen, _ := between(frame, `id="room-generation">`, "</span>")
	n, _ := strconv.Atoi(strings.TrimSpace(gen))
	return rosterEvent{
		Presenter:  presenterFrom(frame),
		Generation: n,
		Names:      membersFrom(frame),
		Raw:        frame,
	}
}

// between returns the text between start and end, and whether both were
// found. One helper for the three cuts below: each extraction is a marker,
// a value, and a terminator.
func between(s, start, end string) (string, bool) {
	_, rest, ok := strings.Cut(s, start)
	if !ok {
		return "", false
	}
	value, _, ok := strings.Cut(rest, end)
	if !ok {
		return "", false
	}
	return value, true
}

// presenterFrom reads the presenter cell, treating the empty-state marker
// as "nobody".
func presenterFrom(frame string) string {
	text, ok := between(frame, `id="room-presenter">`, "</span>")
	if !ok {
		return ""
	}
	text = strings.TrimSpace(text)
	if text == "(none)" {
		return ""
	}
	return text
}

// membersFrom reads every <li> of the roster list, skipping the empty-state
// line and the "(you)" suffix this tab's own row carries.
func membersFrom(frame string) []string {
	list, ok := between(frame, `id="room-roster"`, "</ul>")
	if !ok {
		return nil
	}
	names := make([]string, 0, 4)
	for {
		i := strings.Index(list, "<li")
		if i < 0 {
			return names
		}
		list = list[i:]
		text, more := between(list, ">", "</li>")
		if !more {
			return names
		}
		if name := strings.TrimSpace(text); name != "" && !strings.HasPrefix(name, "Nobody in this room") {
			names = append(names, strings.TrimSuffix(name, " (you)"))
		}
		_, after, _ := strings.Cut(list, "</li>")
		list = after
	}
}

// openRoomStream connects one tab and returns a channel of roster events
// plus a cancel that closes the connection.
func openRoomStream(
	t *testing.T, server *httptest.Server, cookie *http.Cookie, room, clientID string,
) (<-chan rosterEvent, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		server.URL+"/room/stream?room="+room+"&clientID="+clientID, nil)
	if err != nil {
		cancel()
		t.Fatalf("stream request: %v", err)
	}
	req.AddCookie(cookie)
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

	events := make(chan rosterEvent, 16)
	go func() {
		defer close(events)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		// One frame ends at a blank line; only datastar-patch-elements frames
		// carry the roster (signals frames are skipped).
		var cur strings.Builder
		isRoster := false
		for sc.Scan() {
			line := sc.Text()
			if line == "" {
				if isRoster {
					ev := parseRosterFrame(cur.String())
					select {
					case events <- ev:
					case <-ctx.Done():
						return
					}
				}
				cur.Reset()
				isRoster = false
				continue
			}
			if strings.HasPrefix(line, "event: datastar-patch-elements") {
				isRoster = true
			}
			if !strings.HasPrefix(line, "data: ") {
				continue // ": connected" / ": heartbeat" comments, other events
			}
			payload := strings.TrimPrefix(line, "data: ")
			// Strip the Datastar field prefixes so the markup is contiguous.
			for _, p := range []string{"elements ", "mode ", "selector "} {
				if rest, ok := strings.CutPrefix(payload, p); ok {
					payload = rest
					break
				}
			}
			cur.WriteString(payload)
			cur.WriteString("\n")
		}
	}()
	return events, cancel
}

// waitRoster waits for a pushed roster matching pred.
func waitRoster(t *testing.T, events <-chan rosterEvent, pred func(rosterEvent) bool, what string) rosterEvent {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("stream closed while waiting for %s", what)
			}
			if pred(ev) {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// nextRoster returns the very next roster event, whatever it contains.
func nextRoster(t *testing.T, events <-chan rosterEvent, what string) rosterEvent {
	t.Helper()
	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatalf("stream closed while waiting for %s", what)
		}
		return ev
	case <-time.After(20 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		return rosterEvent{}
	}
}

// TestRoomStreamPushesRosterOnConnect pins the handshake: a tab that opens
// the stream is joined to the grain and receives the current card, with no
// polling involved.
func TestRoomStreamPushesRosterOnConnect(t *testing.T) {
	server, signup, _ := roomHostServer(t)
	cookie := signup(roomEmailA)

	events, closeA := openRoomStream(t, server, cookie, "sse-connect", "cliA")
	defer closeA()

	ev := waitRoster(t, events, func(e rosterEvent) bool { return e.has(roomEmailA) }, "the joining member's roster")
	if !strings.Contains(ev.Raw, "room-card") {
		t.Fatalf("handshake frame does not patch the card region: %.200s", ev.Raw)
	}
}

// TestRoomStreamLeavePushesToPeers is THE reason the poll went away: B's
// disconnect must reach A as a roster without B, pushed. No 3s window, no
// heartbeat, no flag.
func TestRoomStreamLeavePushesToPeers(t *testing.T) {
	server, signup, hub := roomHostServer(t)
	cookieA, cookieB := signup(roomEmailA), signup(roomEmailB)

	eventsA, closeA := openRoomStream(t, server, cookieA, "sse-leave", "cliA")
	defer closeA()
	waitRoster(t, eventsA, func(e rosterEvent) bool { return e.has(roomEmailA) }, "A joined")

	_, closeB := openRoomStream(t, server, cookieB, "sse-leave", "cliB")
	defer closeB()
	waitRoster(t, eventsA, func(e rosterEvent) bool {
		return e.has(roomEmailA) && e.has(roomEmailB)
	}, "A to see B join")

	// B's tab goes away. The stream teardown is the leave signal — and it is
	// ASYNCHRONOUS (ServeStream notices the dead socket on its next write),
	// so wait for the hub before asserting on what A received.
	closeB()
	leaveCtx, cancelLeave := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelLeave()
	if err := waitUnregistered(leaveCtx, hub, "cliB"); err != nil {
		t.Fatalf("cliB never unregistered: %v", err)
	}

	waitRoster(t, eventsA, func(e rosterEvent) bool {
		return e.has(roomEmailA) && !e.has(roomEmailB)
	}, "A to see B leave")
}

// TestRoomStreamSecondTabKeepsMember: one person, two streams. Closing one
// must NOT evict them from an observer's roster — only the last may.
//
// AUTHORITY for the refcount is the grain test
// (goakt.TestRoomTwoTabsOneMemberSurvivesOneClosing), which is red-proofed
// against per-name presence. This HTTP test deliberately does NOT claim to
// be: at this layer the teardown is asynchronous, so WHICH pushed roster
// lands next depends on ServeStream's write loop noticing the dead socket
// and on events already buffered for the observer. It is an integration
// check that the stream preserves a member across a tab close.
func TestRoomStreamSecondTabKeepsMember(t *testing.T) {
	server, signup, hub := roomHostServer(t)
	cookieA, cookieB := signup(roomEmailA), signup(roomEmailB)

	eventsA, closeA := openRoomStream(t, server, cookieA, "sse-tabs", "cliA")
	defer closeA()
	waitRoster(t, eventsA, func(e rosterEvent) bool { return e.has(roomEmailA) }, "A joined")

	_, closeB1 := openRoomStream(t, server, cookieB, "sse-tabs", "cliB1")
	defer closeB1()
	eventsB2, closeB2 := openRoomStream(t, server, cookieB, "sse-tabs", "cliB2")
	// Both closes are deferred: a failure before the explicit closeB2 below
	// must not leave a live stream behind, or httptest.Server.Close waits for
	// its handler until the test binary's timeout (lived: a 9-minute hang).
	defer closeB2()
	// BARRIER: B2's own personal card is written AFTER the grain processed
	// its join, so waiting for A's shared roster is not enough — B1 could
	// close while B2's join is still in the mailbox and the grain would see
	// only B1 (evicting B, then re-adding on B2's late join). Wait for B2 to
	// see itself; only then is the refcount guaranteed to be two.
	waitRoster(t, eventsB2, func(e rosterEvent) bool { return e.has(roomEmailB) }, "B2 joined the grain")
	waitRoster(t, eventsA, func(e rosterEvent) bool {
		return e.has(roomEmailA) && e.has(roomEmailB)
	}, "B to join from two tabs")

	closeB1()
	closingCtx, cancelWait := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelWait()
	if err := waitUnregistered(closingCtx, hub, "cliB1"); err != nil {
		t.Fatalf("cliB1 never unregistered: %v", err)
	}
	next := nextRoster(t, eventsA, "a roster after one of two tabs closed")
	if !next.has(roomEmailB) {
		t.Fatalf("member evicted while their second tab was connected: %v", next.Names)
	}

	// The last tab closes: now they really leave.
	closeB2()
	waitRoster(t, eventsA, func(e rosterEvent) bool {
		return e.has(roomEmailA) && !e.has(roomEmailB)
	}, "B to leave after their last connection closed")
}

// TestRoomStreamRequiresSession pins the auth gate: the roster names who is
// in a room and the stream is a live feed of it, so an anonymous tab is
// redirected to sign in exactly like the page.
func TestRoomStreamRequiresSession(t *testing.T) {
	server, _, _ := roomHostServer(t)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		server.URL+"/room/stream?room=lobby&clientID=anon", nil)
	if err != nil {
		t.Fatalf("anon stream request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("anon stream: %v", err)
	}
	defer resp.Body.Close()
	// http.Client follows the 303 to /login; either a redirect status or the
	// login page counts as "not served".
	if resp.StatusCode == http.StatusOK && strings.Contains(resp.Request.URL.Path, "/room/stream") {
		t.Fatalf("anonymous stream served: %d at %s", resp.StatusCode, resp.Request.URL.Path)
	}
}

// TestRoomCrashRejoinsLiveStreams: a crashed grain restarts empty, and
// presence is the CONNECTION — so the handler must hand the fresh grain the
// clientIDs it already has. Without that, pressing crash emptied the roster
// until every tab reloaded (the demo exists to watch it rebuild).
//
// RED-PROOFED: drop the rejoinWatchers call in handleCrash and this times out
// waiting for the member to reappear.
func TestRoomCrashRejoinsLiveStreams(t *testing.T) {
	server, signup, _ := roomHostServer(t)
	cookie := signup(roomEmailA)

	events, closeA := openRoomStream(t, server, cookie, "crash-rejoin", "cliA")
	defer closeA()
	first := waitRoster(t, events, func(e rosterEvent) bool { return e.has(roomEmailA) }, "A to join")

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		server.URL+"/room/crash?room=crash-rejoin", nil)
	if err != nil {
		t.Fatalf("crash request: %v", err)
	}
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("crash call: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("crash status = %d, want 200", resp.StatusCode)
	}

	ev := waitRoster(t, events, func(e rosterEvent) bool {
		return e.Generation > first.Generation && e.has(roomEmailA)
	}, "the restarted roster to include the live tab")
	if len(ev.Names) != 1 {
		t.Fatalf("restarted roster = %v, want just the live tab", ev.Names)
	}
}
