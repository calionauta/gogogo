// SCOPE:feature — HTTP chat contract tests (red-proof).
package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/chat/store"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/queue"
)

// stubResponder answers with canned model output.
type stubResponder struct {
	raw string
	err error
}

func (s stubResponder) Respond(context.Context, string) (string, error) {
	return s.raw, s.err
}

func chatFixture(t *testing.T, responder store.Responder) (http.Handler, *auth.Auth, *store.MemStore) {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/chat.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("db migrate: %v", merr)
	}
	a, err := auth.New(dbgen.New(sqlDB), false)
	if err != nil {
		t.Fatalf("auth new: %v", err)
	}
	q, err := queue.NewAt(t.TempDir() + "/chatqueue")
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	t.Cleanup(q.Close)
	mem := store.NewMemStore()
	h := NewHandler(config.Load(), a, q, queue.NewSSEHub(), mem, responder)
	h.RegisterHandlers(q.Registry())
	q.StartWorkers()
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Handler())
	mux.Handle("/", h.Routes())
	return mux, a, mem
}

func chatSignup(t *testing.T, mux http.Handler, email string) *http.Cookie {
	t.Helper()
	body := `{"email":"` + email + `","password":"Correct-horse-99!"}`
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/auth/signup/credential", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("signup: status = %d", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func TestHandlersChatAuthGate(t *testing.T) {
	mux, _, _ := chatFixture(t, stubResponder{raw: "{}"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/chat", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("anon /chat status = %d, want 303", rec.Code)
	}
}

func TestHandlersChatAskAndAnswer(t *testing.T) {
	mux, a, mem := chatFixture(t, stubResponder{
		raw: `{"components":[{"type":"text_note","props":{"text":"hello there"}}]}`,
	})
	cookie := chatSignup(t, mux, "chat@example.com")

	askRec := httptest.NewRecorder()
	askReq := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/chat/ask?clientID=chat-t1",
		strings.NewReader("prompt=hi+there"))
	askReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	askReq.AddCookie(cookie)
	mux.ServeHTTP(askRec, askReq)
	if askRec.Code != http.StatusOK {
		t.Fatalf("ask status = %d (%s)", askRec.Code, askRec.Body.String())
	}
	if ct := askRec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("ask content-type = %q, want SSE ack", ct)
	}
	// The ack immediately takes the composer: thinking on (double-submit
	// guard) and the prompt cleared (data-bound input follows the merge).
	ack := askRec.Body.String()
	for _, want := range []string{`"chat_thinking":true`, `"chatPrompt":""`} {
		if !strings.Contains(ack, want) {
			t.Errorf("ask ack missing %s (got %.300q…)", want, ack)
		}
	}

	// The worker persists both sides: poll the memory store for the
	// assistant answer with the stubbed text.
	owner := sessionOwner(t, a, cookie)
	deadline := time.Now().Add(8 * time.Second)
	for {
		msgs, err := mem.List(context.Background(), owner)
		if err == nil && len(msgs) == 2 && msgs[1].Text == "hello there" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker never answered (msgs=%+v err=%v)", msgs, err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Stream opens with the thinking flag cleared.
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/chat/stream?clientID=chat-t1", nil)
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream open: %v", err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("stream content-type = %q", ct)
	}
	buf := make([]byte, 8192)
	type readResult struct {
		n   int
		err error
	}
	readCh := make(chan readResult, 1)
	go func() {
		n, err := resp.Body.Read(buf)
		readCh <- readResult{n: n, err: err}
	}()
	select {
	case res := <-readCh:
		if body := string(buf[:res.n]); !strings.Contains(body, "chat_thinking") {
			t.Fatalf("stream handshake missing thinking flag (body: %q)", body)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("stream handshake timed out")
	}
}

// sessionOwner resolves the test user's id through its cookie.
func sessionOwner(t *testing.T, a *auth.Auth, cookie *http.Cookie) string {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	sess, err := a.Session(req)
	if err != nil {
		t.Fatalf("ownerOf: %v", err)
	}
	return sess.User.ID
}

// TestRenderDirectivesSanitizesInventedVisual pins the end of the
// html_block path: worker output re-validated through the catalog
// renders sanitized markup in the bubble — a script in the model's
// answer never reaches the DOM.
func TestRenderDirectivesSanitizesInventedVisual(t *testing.T) {
	t.Parallel()
	raw := []any{map[string]any{
		"Component": "html_block",
		"Props": map[string]any{
			"html": `<div class="stats"><script>alert(1)</script><p>done</p></div>`,
		},
	}}
	comps := renderDirectives(raw)
	if len(comps) != 1 {
		t.Fatalf("renderDirectives(html_block) = %d comps, want 1", len(comps))
	}
	var buf bytes.Buffer
	if err := comps[0].Render(context.Background(), &buf); err != nil {
		t.Fatalf("render html_block: %v", err)
	}
	html := buf.String()
	for _, want := range []string{`class="stats"`, "<p>done</p>"} {
		if !strings.Contains(html, want) {
			t.Errorf("bubble missing %q (got %.200q…)", want, html)
		}
	}
	if strings.Contains(html, "<script") {
		t.Errorf("script survived into the bubble: %.200q…", html)
	}
}

// TestChatFallbackRendersOnce pins the double-render fix: when the model
// answer degrades to prose, the worker stores empty Text with the single
// text_note component — setting both rendered every fallback twice in
// one bubble (raw JSON as paragraph AND as Note card).
func TestChatFallbackRendersOnce(t *testing.T) {
	mux, a, mem := chatFixture(t, stubResponder{raw: `not json at all {{{`})
	cookie := chatSignup(t, mux, "fallback@example.com")

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/chat/ask?clientID=fallback-t1",
		strings.NewReader("prompt=hi"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ask status = %d", rec.Code)
	}
	owner := sessionOwner(t, a, cookie)
	deadline := time.Now().Add(8 * time.Second)
	for {
		msgs, _ := mem.List(context.Background(), owner)
		if len(msgs) == 2 {
			if msgs[1].Text != "" {
				t.Fatalf("fallback Text = %q, want empty (component carries it)", msgs[1].Text)
			}
			if len(msgs[1].Components) != 1 || msgs[1].Components[0].Component != "text_note" {
				t.Fatalf("fallback components = %+v, want 1 text_note", msgs[1].Components)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("worker never answered")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
