// SCOPE:feature — Whiteboard handlers port tests (ported from the legacy whiteboard suite)
//
// The legacy suite pinned the collaborative whiteboard end to end over a live
// SSE stream: session frame, authoritative peer counts, cursor relay with
// string-coord tolerance, server-stamped anti-spoof user, no-echo shape
// broadcast, stale-op 409 with current state, draw-after-clear, offline
// replay, page rendering (doc id, redirect, one "online" label, theme
// toggle wiring) and snapshot persistence. The legacy handler died with the
// framework boot; this file re-pins every one of those contracts against
// the HTTP slice, which serves the same wire protocol.
package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	neturl "net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/queue"
)

const wbEmail = "board@example.com"

// wbPortFixture boots the HTTP slice on a real HTTP server (SSE needs
// one) over an in-memory persister the tests can inspect. Returns the
// server, the persister, and a fresh signed-up session cookie per call.
func wbPortFixture(t *testing.T) (*httptest.Server, *collab.MemoryPersister, *http.Cookie) {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/wb-port.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("db migrate: %v", merr)
	}
	a, aerr := auth.New(dbgen.New(sqlDB), false)
	if aerr != nil {
		t.Fatalf("auth new: %v", aerr)
	}
	mem := collab.NewMemoryPersister()
	h := NewHandler(dbgen.New(sqlDB), a, queue.NewSSEHub(), mem,
		collab.NewDocStore(), config.Load())
	// Transport fixtures drive synthetic doc ids ("note-hammer",
	// "doc-stale-port") on purpose: they exercise the wire, not the index.
	// Opting out HERE, explicitly, is what keeps the write contract on for
	// every real request (see permissions.go). A test that wants the contract
	// must clear this field.
	h.registry = func(context.Context, string) bool { return true }
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Handler())
	mux.Handle("/", h.Routes())
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, mem, wbSignup(t, mux, wbEmail)
}

// wbClient builds a client whose jar already carries the session cookie
// (the signup IS the login: the auth stack auto-opens a session).
func wbClient(t *testing.T, server *httptest.Server, cookie *http.Cookie) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	base, err := neturl.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}
	jar.SetCookies(base, []*http.Cookie{cookie})
	return &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func mustGet(t *testing.T, u string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
	if err != nil {
		t.Fatalf("get req: %v", err)
	}
	return req
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(raw)
}

func postWithClientID(
	ctx context.Context, client *http.Client, u, clientID string, body []byte,
) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u+"?clientID="+clientID,
		strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return client.Do(req)
}

func postOp(t *testing.T, client *http.Client, server *httptest.Server,
	docID, clientID string, op collab.ShapeOp,
) *http.Response {
	t.Helper()
	body, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("marshal op: %v", err)
	}
	resp, err := postWithClientID(context.Background(), client,
		server.URL+"/api/whiteboard/"+docID+"/update", clientID, body)
	if err != nil {
		t.Fatalf("post op: %v", err)
	}
	return resp
}

func whiteboardShapes(t *testing.T, client *http.Client, server *httptest.Server, docID string) []collab.Shape {
	t.Helper()
	resp, err := client.Do(mustGet(t, server.URL+"/api/whiteboard/"+docID+"/snapshot"))
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	defer resp.Body.Close()
	var shapes []collab.Shape
	if err := json.NewDecoder(resp.Body).Decode(&shapes); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	return shapes
}

// --- SSE stream harness (ported from wb_stream_test.go) ---

type wbStream struct {
	resp   *http.Response
	cancel context.CancelFunc
	events chan string
}

func openWBStream(t *testing.T, client *http.Client, server *httptest.Server, docID, clientID string) *wbStream {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		server.URL+"/api/whiteboard/"+docID+"/stream?clientID="+clientID, nil)
	if err != nil {
		t.Fatalf("wb stream req: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("wb stream connect: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("wb stream status = %d", resp.StatusCode)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &wbStream{resp: resp, cancel: cancel, events: make(chan string, 128)}
	go func() {
		defer close(s.events)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		var buf strings.Builder
		for sc.Scan() {
			line := sc.Text()
			if line == "" {
				if buf.Len() > 0 {
					select {
					case s.events <- buf.String():
					case <-ctx.Done():
						return
					}
					buf.Reset()
				}
				continue
			}
			buf.WriteString(line)
			buf.WriteString("\n")
		}
	}()
	return s
}

func (s *wbStream) close() {
	s.cancel()
	_ = s.resp.Body.Close()
}

const wbWaitBudget = 2 * time.Second

func (s *wbStream) drain(window time.Duration) []string {
	var out []string
	timeout := time.After(window)
	for {
		select {
		case ev, ok := <-s.events:
			if !ok {
				return out
			}
			out = append(out, ev)
		case <-timeout:
			return out
		}
	}
}

func (s *wbStream) waitFor(match func(string) bool) []string {
	deadline := time.Now().Add(wbWaitBudget)
	var out []string
	for time.Now().Before(deadline) {
		out = append(out, s.drain(5*time.Millisecond)...)
		if slices.ContainsFunc(out, match) {
			return out
		}
	}
	return out
}

func (s *wbStream) waitForEvent(substr string) {
	s.waitFor(func(ev string) bool { return strings.Contains(ev, substr) })
}

func (s *wbStream) settleJoin() {
	s.waitForEvent(`"type":"count"`)
}

// peersOf returns the roster carried by ONE count frame, or nil for any other
// frame. Parsing a single event keeps the caller honest about which frame it
// read: a helper that takes a whole drained batch and keeps the last count
// silently returns older state when two rosters land in the same 5ms window,
// which is how TestWhiteboard_PeerCountAuthoritative failed in CI (A saw
// [wbB], the frame before its own join had been applied).
func peersOf(ev string) []string {
	raw := strings.TrimPrefix(strings.TrimSpace(ev), "data: ")
	var msg collab.PresenceMsg
	if err := json.Unmarshal([]byte(raw), &msg); err != nil || msg.Type != "count" {
		return nil
	}
	return msg.Peers
}

// firstPeersWith returns the roster of the FIRST count frame in events that
// carries every wanted client id. Pure on purpose: the selection rule is what
// broke, so it is testable without a live stream.
func firstPeersWith(events []string, want ...string) []string {
	for _, ev := range events {
		peers := peersOf(ev)
		if peers == nil {
			continue
		}
		matched := true
		for _, w := range want {
			if !slices.Contains(peers, w) {
				matched = false
				break
			}
		}
		if matched {
			return peers
		}
	}
	return nil
}

// waitForPeers waits until a roster frame contains every wanted client id and
// returns THAT frame's peers. It never returns "the last count seen so far":
// the frame that proved the roster is the answer, not whatever arrived after it.
func (s *wbStream) waitForPeers(want ...string) []string {
	deadline := time.Now().Add(wbWaitBudget)
	for time.Now().Before(deadline) {
		if peers := firstPeersWith(s.drain(5*time.Millisecond), want...); peers != nil {
			return peers
		}
	}
	return nil
}

func cursorFromEvents(events []string) (collab.PresenceMsg, bool) {
	for _, ev := range events {
		raw := strings.TrimPrefix(strings.TrimSpace(ev), "data: ")
		var msg collab.PresenceMsg
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			continue
		}
		if msg.Type == "cursor" {
			return msg, true
		}
	}
	return collab.PresenceMsg{}, false
}

func shapesEventContains(events []string, id string) bool {
	for _, ev := range events {
		var msg collab.WebShapesEvent
		if !strings.Contains(ev, `data: `) {
			continue
		}
		raw := strings.TrimPrefix(strings.TrimSpace(ev), "data: ")
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			continue
		}
		if msg.Type == "shapes" {
			for _, sh := range msg.Shapes {
				if sh.ID == id {
					return true
				}
			}
		}
	}
	return false
}

// tailEvents renders the last few events for failure messages. The window
// is fixed (every caller passed 400): a parameter nobody varies is a
// lie about flexibility, and the unparam gate rejects it.
func tailEvents(events []string) string {
	const n = 400
	if len(events) <= n {
		return strings.Join(events, "\n")
	}
	return strings.Join(events[len(events)-n:], "\n")
}

// --- ported contracts ---
