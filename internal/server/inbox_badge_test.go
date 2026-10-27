// SCOPE:core — Rail inbox count middleware: scope and seeding.
package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/config"
	notificationshandlers "github.com/calionauta/gogogo/features/notifications/handlers"
	"github.com/calionauta/gogogo/internal/auth"
	dbpkg "github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/demo"
	"github.com/calionauta/gogogo/internal/ui"
)

// TestPageRequestScope pins which requests pay for the count query. A page
// navigation renders the rail; an API call, a stream, an asset, the service
// worker, the health probe and the demo cookie do not. Widening this by
// accident adds two queries to every static hit.
func TestPageRequestScope(t *testing.T) {
	cases := []struct {
		method string
		path   string
		want   bool
	}{
		{http.MethodGet, "/todo", true},
		{http.MethodHead, "/notifications", true},
		{http.MethodGet, "/", true},
		{http.MethodGet, "/api/todos", false},
		{http.MethodGet, "/api/notifications/stream", false},
		{http.MethodGet, "/static/app.min.css", false},
		{http.MethodGet, "/sw.js", false},
		{http.MethodGet, "/favicon.ico", false},
		{http.MethodGet, "/health", false},
		{http.MethodGet, "/_ui/demo", false},
		{http.MethodGet, "/console", false},
		{http.MethodGet, "/dagnats", false},
		{http.MethodPost, "/todo", false},
	}
	for _, tc := range cases {
		r := httptest.NewRequestWithContext(context.Background(), tc.method, tc.path, nil)
		if got := pageRequest(r); got != tc.want {
			t.Errorf("pageRequest(%s %s) = %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}

// TestInboxBadgeMiddlewareSeedsTheCount pins that a signed-in page request
// reaches the shell with the owner's unread count on its context — the one
// fact every page's rail renders. A guest gets no seed (zero), because the
// landing page has no account and no inbox.
func TestInboxBadgeMiddlewareSeedsTheCount(t *testing.T) {
	sqlDB, err := dbpkg.OpenAt(filepath.Join(t.TempDir(), "badge.db"))
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("db migrate: %v", merr)
	}
	q := dbgen.New(sqlDB)
	a, aerr := auth.New(q, false)
	if aerr != nil {
		t.Fatalf("auth new: %v", aerr)
	}

	authMux := http.NewServeMux()
	authMux.Handle("/api/auth/", a.Routes())
	srv := httptest.NewServer(authMux)
	t.Cleanup(srv.Close)

	body := `{"email":"badge@demo.app","password":"[LEAKGUARD_REDACTED] — Password Assignment"}`
	req, rerr := http.NewRequestWithContext(context.Background(), http.MethodPost,
		srv.URL+"/api/auth/signup/credential", strings.NewReader(body))
	if rerr != nil {
		t.Fatalf("build signup: %v", rerr)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, serr := http.DefaultClient.Do(req)
	if serr != nil {
		t.Fatalf("signup: %v", serr)
	}
	defer resp.Body.Close()
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatalf("signup status = %d, no %s cookie", resp.StatusCode, auth.CookieName)
	}
	u, uerr := q.GetUserByEmail(context.Background(), "badge@demo.app")
	if uerr != nil {
		t.Fatalf("get user: %v", uerr)
	}
	if _, cerr := q.CreateNotification(context.Background(), dbgen.CreateNotificationParams{
		ID: dbschema.NewID(), Owner: u.ID, Workflow: "reminder",
		TransactionID: "badge:1", Channel: "in_app", Status: "sent",
		Provider: "local", Subject: "S", Body: "B", Created: dbschema.Now(),
	}); cerr != nil {
		t.Fatalf("create notification: %v", cerr)
	}

	var got int
	mw := inboxBadgeMiddleware(a, q)
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = ui.InboxUnread(r.Context())
	}))

	signed := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/todo", nil)
	signed.AddCookie(cookie)
	h.ServeHTTP(httptest.NewRecorder(), signed)
	if got != 1 {
		t.Fatalf("InboxUnread = %d, want 1 for the owner's unread row", got)
	}

	guest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	h.ServeHTTP(httptest.NewRecorder(), guest)
	if got != 0 {
		t.Fatalf("guest InboxUnread = %d, want 0", got)
	}
}

// sessionWireFrame is the subset of /api/session/stream the badge consumes.
type sessionWireFrame struct {
	Type   string `json:"type"`
	Authed bool   `json:"authed"`
	Unread *int   `json:"unread,omitempty"`
}

// scanSessionFrames decodes the stream off the response body in the
// background, so a test can bound each wait instead of hanging on a read.
func scanSessionFrames(body io.Reader) <-chan sessionWireFrame {
	frames := make(chan sessionWireFrame, 16)
	go func() {
		defer close(frames)
		sc := bufio.NewScanner(body)
		sc.Buffer(make([]byte, 0, 4096), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var f sessionWireFrame
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &f); err != nil {
				continue
			}
			frames <- f
		}
	}()
	return frames
}

// awaitUnread reads frames until one carries the wanted count.
func awaitUnread(t *testing.T, frames <-chan sessionWireFrame, want int) sessionWireFrame {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case f, ok := <-frames:
			if !ok {
				t.Fatalf("stream closed before unread %d", want)
			}
			if f.Unread != nil && *f.Unread == want {
				return f
			}
		case <-deadline:
			t.Fatalf("timed out waiting for unread %d on /api/session/stream", want)
		}
	}
}

// bootUnreadApp boots the production composition (demo accounts, no engines)
// and signs in as the demo admin. The caller gets the base URL, the session
// cookie and the stack, because the badge path spans composition wiring, the
// auth stream and the notifications bus.
func bootUnreadApp(t *testing.T) (string, *http.Cookie, *ChiStack) {
	t.Helper()
	cfg := config.Load()
	cfg.Dev = true
	cfg.DemoAccounts = true
	cfg.RBACEnabled = false
	cfg.NATS.Enabled = false
	cfg.DagNats.Enabled = false
	cfg.GoAkt.Enabled = false
	tmp := t.TempDir()
	cfg.Turso.DBPath = filepath.Join(tmp, "unread.db")
	cfg.DataDir = filepath.Join(tmp, "data")

	stack, err := BootChi(cfg)
	if err != nil {
		t.Fatalf("BootChi: %v", err)
	}
	ln, lerr := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if lerr != nil {
		t.Fatalf("listen: %v", lerr)
	}
	t.Cleanup(serveChiOnListener(ln, srvHandler(stack.mux), stack))
	base := "http://" + ln.Addr().String()

	body, _ := json.Marshal(map[string]string{
		"credential": demo.Email,
		"password":   demo.Password,
	})
	req, rerr := http.NewRequestWithContext(context.Background(), http.MethodPost,
		base+"/api/auth/signin/credential", strings.NewReader(string(body)))
	if rerr != nil {
		t.Fatalf("sign-in request: %v", rerr)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, serr := http.DefaultClient.Do(req)
	if serr != nil {
		t.Fatalf("sign in: %v", serr)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sign in status = %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName {
			return base, c, stack
		}
	}
	t.Fatalf("sign in set no %s cookie", auth.CookieName)
	return "", nil, nil
}

// openSessionFeed opens /api/session/stream with the cookie and returns the
// frame channel plus the first frame.
func openSessionFeed(t *testing.T, base string, cookie *http.Cookie) (<-chan sessionWireFrame, sessionWireFrame) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/session/stream", nil)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	req.AddCookie(cookie)
	resp, derr := http.DefaultClient.Do(req)
	if derr != nil {
		t.Fatalf("open stream: %v", derr)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", resp.StatusCode)
	}
	frames := scanSessionFrames(resp.Body)
	first, ok := <-frames
	if !ok {
		t.Fatal("stream sent no opening frame")
	}
	return frames, first
}

// TestSessionStreamPushesUnreadFromTheInboxBus is the end-to-end proof of the
// rail badge's live path: the leader stream opens with the current count, and
// a delivery through the notifications bus pushes a new frame (the same frame
// session.js fans out to every tab). It also proves BootChi wires the feed —
// without SetSessionStreamExtras the frame never carries unread.
func TestSessionStreamPushesUnreadFromTheInboxBus(t *testing.T) {
	base, cookie, stack := bootUnreadApp(t)
	frames, first := openSessionFeed(t, base, cookie)
	if first.Type != "session" || !first.Authed || first.Unread == nil {
		t.Fatalf("opening frame = %+v, want {session true unread set}", first)
	}

	admin, uerr := stack.queries.GetUserByEmail(context.Background(), demo.Email)
	if uerr != nil {
		t.Fatalf("demo admin: %v", uerr)
	}
	if derr := stack.NotificationsHandler.Deliver(context.Background(), notificationshandlers.Delivery{
		Owner: admin.ID, TransactionID: "session-stream:1", Workflow: "reminder",
		Subject: "Rail badge", Body: "Pushed over the leader stream",
	}); derr != nil {
		t.Fatalf("deliver: %v", derr)
	}
	awaitUnread(t, frames, *first.Unread+1)
}
