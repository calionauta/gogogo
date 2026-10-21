// SCOPE:feature — HTTP chat contract tests (red-proof).
package handlers

import (
	"bytes"
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/a-h/templ"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/chat/store"
	comp "github.com/calionauta/gogogo/features/genui/components"
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

// panelRowStub is the stub's row shape, deliberately local: the chat handler
// never learns what a todo is, only that something can fill a panel's rows, so
// proving that means NOT importing a feature package here.
type panelRowStub struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// stubPanel stands in for a feature's panel provider.
type stubPanel struct {
	rows []panelRowStub
	err  error
}

func (s stubPanel) Component() string { return "todo_list" }

func (s stubPanel) Prompt() string { return "todo_list {filter} (stub)" }

func (s stubPanel) Writable() []string { return []string{"filter"} }

func (s stubPanel) Render(props map[string]any) (templ.Component, error) {
	filter, _ := props["filter"].(string)
	rows, _ := props["rows"].([]panelRowStub)
	var b strings.Builder
	b.WriteString(`<div data-filter="` + filter + `">`)
	for _, r := range rows {
		b.WriteString("<span>" + r.Title + "</span>")
	}
	b.WriteString("</div>")
	return templ.Raw(b.String()), nil
}

func (s stubPanel) Fill(_ context.Context, _ string, props map[string]any) (map[string]any, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make(map[string]any, len(props)+1)
	maps.Copy(out, props)
	out["rows"] = s.rows
	return out, nil
}

func chatFixture(t *testing.T, responder store.Responder) (http.Handler, *auth.Auth, *store.MemStore) {
	t.Helper()
	return chatFixtureWith(t, responder, nil)
}

// chatFixtureWith is chatFixture with a panel provider installed, so a test can
// prove a system panel renders the store's rows.
//
// `panels` installs the PROCESS registry (there is no per-handler list any
// more: the catalog is what the prompt advertises, and a handler-local copy
// could disagree with it). A nil slice installs the fixture's default stub, so
// every test in this package sees the same vocabulary the boot would give it.
func chatFixtureWith(
	t *testing.T, responder store.Responder, panels []comp.PanelProvider,
) (http.Handler, *auth.Auth, *store.MemStore) {
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
	if panels == nil {
		panels = []comp.PanelProvider{stubPanel{}}
	}
	if err := comp.InstallPanelRegistry(panels); err != nil {
		t.Fatalf("install panel registry: %v", err)
	}
	mem := store.NewMemStore()
	h := NewHandler(config.Load(), a, q, queue.NewSSEHub(), mem, responder)
	h.RegisterHandlers(q.Registry())
	q.StartWorkers()
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Routes())
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
	// The ack raises the double-submit guard but does NOT touch the composer:
	// $chatPrompt is bound two-way to the input, so the box owns it, and a
	// response that cleared it wiped text typed after submit (reproduced in
	// the browser). The client clears it on success; asserting its absence is
	// what keeps the bug from coming back.
	ack := askRec.Body.String()
	for _, want := range []string{`"chat_thinking":true`} {
		if !strings.Contains(ack, want) {
			t.Errorf("ask ack missing %s (got %.300q…)", want, ack)
		}
	}
	if strings.Contains(ack, `"chatPrompt"`) {
		t.Errorf("ask ack wrote the client-owned chatPrompt signal; a response "+
			"landing after the user typed would erase the composer (got %.300q…)", ack)
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
	// The stream opens with the ack frame (streamOpen), then the handshake
	// signals. Read until the handshake lands instead of assuming one read
	// carries both.
	buf := make([]byte, 8192)
	type readResult struct {
		n   int
		err error
	}
	readDeadline := time.After(4 * time.Second)
	var body strings.Builder
	for {
		readCh := make(chan readResult, 1)
		go func() {
			n, err := resp.Body.Read(buf)
			readCh <- readResult{n: n, err: err}
		}()
		select {
		case res := <-readCh:
			body.Write(buf[:res.n])
			if strings.Contains(body.String(), "chat_thinking") {
				return
			}
			if res.err != nil {
				t.Fatalf("stream closed before the handshake (body: %q)", body.String())
			}
		case <-readDeadline:
			t.Fatalf("stream handshake timed out (body: %q)", body.String())
		}
	}
}

// TestHandlersChatEchoesPromptAsOwnBubble pins the live conversation
// contract: the ask ack APPENDS the user's own bubble into
// #chat-messages before the worker answers. The appended fragment must
// not carry the container id itself — a server-side patch targets every
// match of its selector, so a nested #chat-messages doubled each answer.
func TestHandlersChatEchoesPromptAsOwnBubble(t *testing.T) {
	mux, _, _ := chatFixture(t, stubResponder{raw: `{"components":[{"type":"text_note","props":{"text":"ok"}}]}`})
	cookie := chatSignup(t, mux, "echo@example.com")

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/chat/ask?clientID=echo-t1",
		strings.NewReader("prompt=plan+my+day"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ask status = %d (%s)", rec.Code, rec.Body.String())
	}
	ack := rec.Body.String()
	for _, want := range []string{
		"datastar-patch-elements",
		"selector #chat-messages",
		"mode append",
		"plan my day",
		"chat-end",
	} {
		if !strings.Contains(ack, want) {
			t.Errorf("ask ack missing %q (got %.400q…)", want, ack)
		}
	}
	if strings.Contains(ack, `id="chat-messages"`) {
		t.Errorf("the appended fragment carries the container id (nested #chat-messages): %.400q…", ack)
	}
}

// TestHandlersChatRendersHistory pins the reload path: the page is built
// from the store, so a conversation survives a refresh, and the first-run
// guidance is driven by the seeded $chat_empty signal.
func TestHandlersChatRendersHistory(t *testing.T) {
	mux, a, mem := chatFixture(t, stubResponder{raw: "{}"})
	cookie := chatSignup(t, mux, "history@example.com")
	owner := sessionOwner(t, a, cookie)
	ctx := context.Background()
	if err := mem.Append(ctx, owner, store.Message{Role: "user", Text: "first question"}); err != nil {
		t.Fatal(err)
	}
	if err := mem.Append(ctx, owner, store.Message{Role: "assistant", Text: "first answer"}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/chat", nil)
	req.AddCookie(cookie)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /chat status = %d", rec.Code)
	}
	html := html.UnescapeString(rec.Body.String())
	for _, want := range []string{"first question", "first answer", `"chat_empty":false`, `id="chat-empty"`} {
		if !strings.Contains(html, want) {
			t.Errorf("/chat missing %q", want)
		}
	}
	if got := strings.Count(html, `id="chat-messages"`); got != 1 {
		t.Errorf("/chat holds %d #chat-messages containers, want exactly 1", got)
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
