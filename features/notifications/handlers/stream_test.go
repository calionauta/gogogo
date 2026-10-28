// SCOPE:feature — notifications: the live inbox stream, end to end.
package handlers

import (
	"bufio"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

	// Handshake first: the ack, the initial inbox frame, and the shell count
	// signal (the sidebar badge rides the same stream, so a delivery updates it
	// without a reload).
	waitFor("streamOpen")
	waitFor("Nothing here yet.")
	waitFor("inboxUnread")

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

// gatedSSE blocks the handler at the handshake's inbox frame, so a test can
// deliver in the window between "the client sees the initial state" and the
// bus subscription. That window is exactly what the live-delivery test lost
// under the race detector: the handler subscribed after writing the handshake.
type gatedSSE struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	hdr      http.Header
	gateOnce sync.Once
	gate     chan struct{}
	started  chan struct{}
}

func newGatedSSE() *gatedSSE {
	return &gatedSSE{
		hdr:     make(http.Header),
		gate:    make(chan struct{}),
		started: make(chan struct{}),
	}
}

func (g *gatedSSE) Header() http.Header { return g.hdr }
func (g *gatedSSE) WriteHeader(int)     {}
func (g *gatedSSE) Flush()              {}

func (g *gatedSSE) Write(p []byte) (int, error) {
	g.mu.Lock()
	n, err := g.buf.Write(p)
	body := g.buf.String()
	g.mu.Unlock()
	if strings.Contains(body, "Nothing here yet.") {
		g.gateOnce.Do(func() {
			close(g.started)
			<-g.gate
		})
	}
	return n, err
}

func (g *gatedSSE) String() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.buf.String()
}

// TestStreamSubscribesBeforeTheHandshake is the deterministic form of the
// live-delivery regression: the handler is stopped with the initial inbox
// frame already written (the client's "ready"), a delivery is published, and
// only then is it released. A subscription created after the handshake misses
// that event; one created before it buffers the wake and patches the row.
func TestStreamSubscribesBeforeTheHandshake(t *testing.T) {
	h, q, _ := newFixture(t)
	_, cookie := signedInServer(t, h, "order@demo.app")
	u, err := q.GetUserByEmail(context.Background(), "order@demo.app")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet,
		"/api/notifications/stream?clientID=tab-test", nil)
	req.AddCookie(cookie)
	gate := newGatedSSE()
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.handleStream(gate, req)
	}()

	select {
	case <-gate.started:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never wrote the initial inbox frame")
	}
	if err := h.Deliver(context.Background(), Delivery{
		Owner: u.ID, TransactionID: "order:1", Workflow: "reminder",
		Subject: "Delivered in the gap", Body: "must not be lost",
	}); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	close(gate.gate)

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(gate.String(), "Delivered in the gap") {
		if time.Now().After(deadline) {
			t.Fatalf("delivery was lost between the handshake and the subscription:\n%s", gate.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not stop")
	}
}
