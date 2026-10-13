// SCOPE:feature — Notes handlers port tests (ported from the legacy notes suite)
//
// The legacy suite pinned ~20 behaviours of the notes slice over a live SSE
// stream: caret atomicity, presence join/leave counts, typing relay,
// session visibility, rev chain, stale-base 409, clamped inserts,
// snapshot persistence, page rendering and the mirror-div recipe. The
// legacy handler died with the framework boot; this file re-pins every one
// of those contracts against the HTTP slice, which serves the same wire
// protocol over the same DocStore/hub.
package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
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

const notesEmail = "notes@example.com"

// notesPortFixture boots the HTTP slice on a real HTTP server (SSE needs
// one) over an in-memory persister the tests can inspect. Returns the
// server, the persister, and one authed session cookie.
func notesPortFixture(t *testing.T) (*httptest.Server, *collab.MemoryPersister, *http.Cookie) {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/notes-port.db")
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
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Handler())
	mux.Handle("/", h.Routes())
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, mem, notesSignup(t, mux, notesEmail)
}

// notesAuthedClient returns a client carrying the signup session cookie.
func notesAuthedClient(t *testing.T, server *httptest.Server, cookie *http.Cookie) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}
	jar.SetCookies(base, []*http.Cookie{cookie})
	return &http.Client{Jar: jar}
}

// eventLog is a lossless transcript of one SSE stream. Every payload the
// scanner reads is appended; waiters scan the whole log instead of
// consuming a channel, so an early wait can never eat an event a later
// wait needs.
type eventLog struct {
	mu     sync.Mutex
	events []map[string]any
}

func (l *eventLog) append(data string) {
	var msg map[string]any
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, msg)
}

func (l *eventLog) find(pred func(map[string]any) bool) (map[string]any, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, msg := range l.events {
		if pred(msg) {
			return msg, true
		}
	}
	return nil, false
}

func (l *eventLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.events)
}

func waitNoteEvent(t *testing.T, log *eventLog, pred func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if msg, ok := log.find(pred); ok {
			return msg
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for matching event (%d arrived)", log.count())
	return nil
}

// settleStream waits for the stream's OWN authoritative count event,
// proving the server registered this client before the test acts.
func settleStream(t *testing.T, log *eventLog, clientID string) {
	t.Helper()
	waitNoteEvent(t, log, func(msg map[string]any) bool {
		return msg["type"] == "count" && containsAll(peerIDs(msg), clientID)
	})
}

func peerIDs(msg map[string]any) []string {
	var out []string
	if peers, ok := msg["peers"].([]any); ok {
		for _, p := range peers {
			if s, ok := p.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func containsAll(hay []string, needles ...string) bool {
	set := map[string]bool{}
	for _, h := range hay {
		set[h] = true
	}
	for _, n := range needles {
		if !set[n] {
			return false
		}
	}
	return true
}

// openNoteStream opens the SSE stream for docID and records its events.
func openNoteStream(
	t *testing.T, client *http.Client, server *httptest.Server, docID, clientID string,
) (*eventLog, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		server.URL+"/api/notes/"+docID+"/stream?clientID="+clientID, nil)
	if err != nil {
		cancel()
		t.Fatalf("stream req: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("stream open: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		cancel()
		resp.Body.Close()
		t.Fatalf("stream status = %d", resp.StatusCode)
	}
	log := &eventLog{}
	go func() {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			if rest, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				log.append(rest)
			}
		}
	}()
	return log, func() { cancel(); resp.Body.Close() }
}

func postOp(t *testing.T, client *http.Client, server *httptest.Server,
	docID, clientID, body string,
) (int, map[string]any) {
	t.Helper()
	code, out, err := postOpRaw(client, server, docID, clientID, body)
	if err != nil {
		t.Fatalf("op post: %v", err)
	}
	return code, out
}

func postOpRaw(client *http.Client, server *httptest.Server,
	docID, clientID, body string,
) (int, map[string]any, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		server.URL+"/api/notes/"+docID+"/op?clientID="+clientID,
		strings.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	var out map[string]any
	if dErr := json.NewDecoder(resp.Body).Decode(&out); dErr != nil {
		return resp.StatusCode, map[string]any{}, nil
	}
	return resp.StatusCode, out, nil
}

func postPresence(t *testing.T, client *http.Client, server *httptest.Server, docID, clientID, path, body string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		server.URL+"/api/notes/"+docID+"/"+path+"?clientID="+clientID,
		strings.NewReader(body))
	if err != nil {
		t.Fatalf("%s req: %v", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s post: %v", path, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func doGet(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("get req: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	return resp
}

func createNoteForTest(t *testing.T, client *http.Client, server *httptest.Server) string {
	t.Helper()
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	defer func() { client.CheckRedirect = nil }()
	resp := doGet(t, client, server.URL+"/notes/new")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("new note status = %d", resp.StatusCode)
	}
	id := strings.TrimPrefix(resp.Header.Get("Location"), "/notes/")
	if id == "" || strings.Contains(id, "/") {
		t.Fatalf("new note Location = %q", resp.Header.Get("Location"))
	}
	return id
}
