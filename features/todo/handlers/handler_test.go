// SCOPE:feature — HTTP todo slice contract tests (red-proof for step 4).
//
// Every test boots a real migrated Turso file, signs a user up through the
// real auth HTTP surface, and drives the HTTP todo routes over HTTP. The
// contracts cover what the client can observe: paths, status codes,
// redirects, SSE content type, the #todo-list fragment HTML, and owner
// isolation.
package handlers

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/demo"
	"github.com/calionauta/gogogo/internal/queue"
)

// handlersFixture boots the full stack in-process: Turso + migrations, owned
// auth, a private SSE hub, and the chi router with auth + todo routes.
type handlersFixture struct {
	t     testing.TB
	r     http.Handler
	h     *Handler
	q     *dbgen.Queries
	sqlDB *sql.DB
	hub   *queue.SSEHub
	auth  *auth.Auth
}

func newHandlersFixture(tb testing.TB) *handlersFixture {
	tb.Helper()

	sqlDB, err := db.OpenAt(tb.TempDir() + "/todo-handlers.db")
	if err != nil {
		tb.Fatalf("db open: %v", err)
	}
	tb.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		tb.Fatalf("db migrate: %v", merr)
	}

	a, err := auth.New(dbgen.New(sqlDB), false)
	if err != nil {
		tb.Fatalf("auth new: %v", err)
	}

	hub := queue.NewSSEHub()
	h := NewHandler(dbgen.New(sqlDB), a, hub)
	h.SetDatabase(sqlDB)

	r := http.NewServeMux()
	// The auth handler already carries its /api/auth base path and the chi
	// router serves absolute paths, so both mount without prefix games;
	// the more specific /api/auth/ wins over the HTTP root mount.
	r.Handle("/api/auth/", a.Routes())
	r.Handle("/", h.Routes())
	return &handlersFixture{
		t: tb, r: r, h: h, q: dbgen.New(sqlDB), sqlDB: sqlDB,
		hub: hub, auth: a,
	}
}

// signup creates a user over HTTP and returns the session cookie.
func (f *handlersFixture) signup(email string) *http.Cookie {
	f.t.Helper()
	body := `{"email":` + strconv.Quote(email) + `,"password":"Correct-horse-99!"}`
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/auth/signup/credential", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		f.t.Fatalf("signup %s: status = %d (body: %s)", email, rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName {
			return c
		}
	}
	f.t.Fatalf("signup %s: no %q cookie", email, auth.CookieName)
	return nil
}

// do drives one request. `extra` carries request-scoped cookies the test needs
// beyond the session — today only the demo-mode cookie, because the module
// machinery (workflow tab, AI suggest) renders in the state panel, which the
// shell docks only in demo mode.
func (f *handlersFixture) do(
	method, target, form string, cookie *http.Cookie, extra ...*http.Cookie,
) *httptest.ResponseRecorder {
	f.t.Helper()
	var reader io.Reader
	if form != "" {
		reader = strings.NewReader(form)
	}
	req := httptest.NewRequestWithContext(context.Background(), method, target, reader)
	if form != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	for _, c := range extra {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	return rec
}

func TestHandlersTodoRequiresAuth(t *testing.T) {
	f := newHandlersFixture(t)
	for _, tc := range []struct{ method, target string }{
		{http.MethodGet, "/api/todos"},
		{http.MethodGet, "/api/todos/fragment"},
		{http.MethodPost, "/api/todos"},
	} {
		rec := f.do(tc.method, tc.target, "title=x", nil)
		if rec.Code != http.StatusSeeOther {
			t.Errorf("%s %s anon: status = %d, want 303", tc.method, tc.target, rec.Code)
		}
	}
}

func TestHandlersTodoCRUDFlow(t *testing.T) {
	f := newHandlersFixture(t)
	cookie := f.signup("crud@example.com")

	// Create returns an SSE stream patching the list.
	rec := f.do(http.MethodPost, "/api/todos", "title=Buy+milk&filter=all", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("create: content-type = %q, want SSE", ct)
	}

	// The fragment lists the new row.
	frag := f.do(http.MethodGet, "/api/todos/fragment?filter=all", "", cookie)
	if frag.Code != http.StatusOK {
		t.Fatalf("fragment: status = %d", frag.Code)
	}
	if !strings.Contains(frag.Body.String(), "Buy milk") {
		t.Fatalf("fragment missing created todo (body: %s)", frag.Body.String())
	}

	// Find the id via the store, toggle it, delete it.
	ctx := context.Background()
	owner := f.ownerOf(t, cookie)
	list, err := f.q.ListTodosByOwner(ctx, owner)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %d rows, %v; want 1", len(list), err)
	}
	id := list[0].ID

	toggle := f.do(http.MethodPost, "/api/todos/"+id+"/toggle?filter=all", "", cookie)
	if toggle.Code != http.StatusOK {
		t.Fatalf("toggle: status = %d (body: %s)", toggle.Code, toggle.Body.String())
	}
	got, err := f.q.GetTodo(ctx, dbgen.GetTodoParams{ID: id, Owner: owner})
	if err != nil || !got.Completed {
		t.Fatalf("after toggle completed = %+v, %v; want true", got, err)
	}

	del := f.do(http.MethodPost, "/api/todos/"+id+"/delete?filter=all", "", cookie)
	if del.Code != http.StatusOK {
		t.Fatalf("delete: status = %d (body: %s)", del.Code, del.Body.String())
	}
	if n, _ := f.q.CountTodosByOwner(ctx, owner); n != 0 {
		t.Fatalf("after delete count = %d, want 0", n)
	}
}

func TestHandlersTodoOwnerIsolation(t *testing.T) {
	f := newHandlersFixture(t)
	a := f.signup("a@example.com")
	b := f.signup("b@example.com")

	f.do(http.MethodPost, "/api/todos", "title=A-secret&filter=all", a)
	ctx := context.Background()
	ownerA := f.ownerOf(t, a)
	list, err := f.q.ListTodosByOwner(ctx, ownerA)
	if err != nil || len(list) != 1 {
		t.Fatalf("setup list = %d, %v", len(list), err)
	}
	id := list[0].ID

	// B cannot read, toggle, or delete A's todo.
	if rec := f.do(http.MethodPost, "/api/todos/"+id+"/toggle", "", b); rec.Code != http.StatusNotFound {
		t.Errorf("cross-owner toggle: status = %d, want 404", rec.Code)
	}
	if rec := f.do(http.MethodPost, "/api/todos/"+id+"/delete", "", b); rec.Code != http.StatusNotFound {
		t.Errorf("cross-owner delete: status = %d, want 404", rec.Code)
	}
	// B's own list is empty.
	frag := f.do(http.MethodGet, "/api/todos/fragment?filter=all", "", b)
	if strings.Contains(frag.Body.String(), "A-secret") {
		t.Error("B's fragment leaks A's todo")
	}
}

func TestHandlersTodoIdempotentCreate(t *testing.T) {
	f := newHandlersFixture(t)
	cookie := f.signup("idem@example.com")

	first := f.do(http.MethodPost, "/api/todos", "title=Once&idem_key=replay-1&filter=all", cookie)
	if first.Code != http.StatusOK {
		t.Fatalf("first create: status = %d", first.Code)
	}
	second := f.do(http.MethodPost, "/api/todos", "title=Once&idem_key=replay-1&filter=all", cookie)
	if second.Code != http.StatusOK {
		t.Fatalf("replay create: status = %d", second.Code)
	}
	ctx := context.Background()
	if n, _ := f.q.CountTodosByOwner(ctx, f.ownerOf(t, cookie)); n != 1 {
		t.Fatalf("after replay count = %d, want 1", n)
	}
}

func TestHandlersTodoClearCompleted(t *testing.T) {
	f := newHandlersFixture(t)
	cookie := f.signup("clear@example.com")

	f.do(http.MethodPost, "/api/todos", "title=Done-item&filter=all", cookie)
	f.do(http.MethodPost, "/api/todos", "title=Open-item&filter=all", cookie)
	ctx := context.Background()
	owner := f.ownerOf(t, cookie)
	list, _ := f.q.ListTodosByOwner(ctx, owner)
	for _, item := range list {
		if item.Title == "Done-item" {
			if err := f.q.SetTodoCompleted(ctx, dbgen.SetTodoCompletedParams{
				Completed: true, Updated: dbschema.Now(), ID: item.ID, Owner: owner,
			}); err != nil {
				t.Fatalf("setup toggle: %v", err)
			}
		}
	}

	rec := f.do(http.MethodPost, "/api/todos/completed/delete?filter=all", "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear: status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	rest, _ := f.q.ListTodosByOwner(ctx, owner)
	if len(rest) != 1 || rest[0].Title != "Open-item" {
		t.Fatalf("after clear = %+v, want only Open-item", rest)
	}
}

func TestHandlersTodoStreamOpens(t *testing.T) {
	f := newHandlersFixture(t)
	cookie := f.signup("stream@example.com")

	// A real TCP server: httptest.ResponseRecorder is not safe for
	// concurrent read-while-streaming, the production writer is.
	srv := httptest.NewServer(f.r)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/todos/stream?clientID=test-stream-1", nil)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream open: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("stream content-type = %q, want text/event-stream", ct)
	}
	// The first frame is the open ack (streamOpen); the handshake signals
	// follow, so read until they land instead of assuming one read.
	buf := make([]byte, 4096)
	type readResult struct {
		n   int
		err error
	}
	readDeadline := time.After(4 * time.Second)
	var body strings.Builder
	for !strings.Contains(body.String(), "itemCount") {
		readCh := make(chan readResult, 1)
		go func() {
			n, err := resp.Body.Read(buf)
			readCh <- readResult{n: n, err: err}
		}()
		select {
		case res := <-readCh:
			body.Write(buf[:res.n])
			if res.err != nil && !strings.Contains(body.String(), "itemCount") {
				t.Fatalf("stream closed before the handshake (body: %q)", body.String())
			}
		case <-readDeadline:
			t.Fatalf("stream handshake timed out (body: %q)", body.String())
		}
	}
}

// ownerOf resolves the test user's integer owner id through its session.
func (f *handlersFixture) ownerOf(t *testing.T, cookie *http.Cookie) string {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	sess, err := f.auth.Session(req)
	if err != nil {
		t.Fatalf("ownerOf: %v", err)
	}
	return sess.User.ID
}

// demoCookie turns demo mode on for one request: that is where the module's
// machinery renders (the shell docks #state-panel only then).
func demoCookie() *http.Cookie {
	return &http.Cookie{
		Name: demo.Cookie, Value: "1",
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	}
}
