package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/features/chat/store"
	"github.com/calionauta/gogogo/internal/auth"
)

func TestHandlersChatMessagesReadPath(t *testing.T) {
	mux, a, mem := chatFixture(t, stubResponder{raw: "{}"})
	cookie := chatSignup(t, mux, "reader@example.com")
	other := chatSignup(t, mux, "reader-other@example.com")
	owner := sessionOwner(t, a, cookie)
	ctx := context.Background()
	for _, m := range []store.Message{
		{Role: "user", Text: "first", IdemKey: "r1"},
		{Role: "assistant", Text: "answer", IdemKey: "r2"},
		{Role: "user", Text: "third", IdemKey: "r3"},
	} {
		if err := mem.Append(ctx, owner, m); err != nil {
			t.Fatal(err)
		}
	}
	otherOwner := sessionOwner(t, a, other)
	if err := mem.Append(ctx, otherOwner, store.Message{Role: "user", Text: "theirs", IdemKey: "r4"}); err != nil {
		t.Fatal(err)
	}

	anonRec := httptest.NewRecorder()
	mux.ServeHTTP(anonRec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/chat/messages", nil))
	if anonRec.Code != http.StatusUnauthorized {
		t.Fatalf("anon history status = %d, want 401", anonRec.Code)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/chat/messages", nil)
	req.AddCookie(cookie)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("history status = %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Messages []historyMessage `json:"messages"`
		Count    int              `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("history body: %v", err)
	}
	if body.Count != 3 || len(body.Messages) != 3 {
		t.Fatalf("history = %+v, want the caller's 3 messages only", body)
	}
	if body.Messages[1].Role != "assistant" || body.Messages[1].Text != "answer" {
		t.Fatalf("history order/content = %+v", body.Messages)
	}

	limRec := httptest.NewRecorder()
	limReq := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/chat/messages?limit=1", nil)
	limReq.AddCookie(cookie)
	mux.ServeHTTP(limRec, limReq)
	if limRec.Code != http.StatusOK {
		t.Fatalf("limited history status = %d", limRec.Code)
	}
	var limited struct {
		Messages []historyMessage `json:"messages"`
	}
	if err := json.Unmarshal(limRec.Body.Bytes(), &limited); err != nil {
		t.Fatalf("limited body: %v", err)
	}
	if len(limited.Messages) != 1 || limited.Messages[0].Text != "third" {
		t.Fatalf("limit=1 = %+v, want only the newest message", limited.Messages)
	}

	badRec := httptest.NewRecorder()
	badReq := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/chat/messages?limit=0", nil)
	badReq.AddCookie(cookie)
	mux.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("limit=0 status = %d, want 400", badRec.Code)
	}
}

// TestHandlersChatClearIsNewChat pins the new-chat affordance: POST
// /api/chat/clear drops only the caller's thread, answers SSE with the
// empty-state signals, and swaps the column for the empty guidance —
// without touching the client-owned $chatPrompt (see the ask-ack
// contract: the input owns it, the server never writes it).
func TestHandlersChatClearIsNewChat(t *testing.T) {
	mux, a, mem := chatFixture(t, stubResponder{raw: "{}"})
	cookie := chatSignup(t, mux, "clear@example.com")
	otherCookie := chatSignup(t, mux, "clear-other@example.com")
	owner := sessionOwner(t, a, cookie)
	other := sessionOwner(t, a, otherCookie)
	ctx := context.Background()
	if err := mem.Append(ctx, owner, store.Message{Role: "user", Text: "mine", IdemKey: "k1"}); err != nil {
		t.Fatal(err)
	}
	if err := mem.Append(ctx, other, store.Message{Role: "user", Text: "theirs", IdemKey: "k2"}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/chat/clear?clientID=clear-t1", nil)
	req.AddCookie(cookie)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear status = %d (%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("clear content-type = %q, want SSE ack", ct)
	}
	if msgs, err := mem.List(ctx, owner); err != nil || len(msgs) != 0 {
		t.Fatalf("owner thread after clear = %+v err=%v, want empty", msgs, err)
	}
	if msgs, err := mem.List(ctx, other); err != nil || len(msgs) != 1 {
		t.Fatalf("other thread after clear = %+v err=%v, want untouched", msgs, err)
	}
	ack := rec.Body.String()
	for _, want := range []string{
		`"chat_empty":true`, "chat_thinking", "datastar-patch-elements",
		"selector #chat-messages", "Ask your app anything",
	} {
		if !strings.Contains(ack, want) {
			t.Errorf("clear ack missing %q (got %.400q…)", want, ack)
		}
	}
	if strings.Contains(ack, `"chatPrompt"`) {
		t.Errorf("clear ack writes the client-owned chatPrompt signal (got %.400q…)", ack)
	}
	if strings.Contains(ack, "mode append") {
		t.Errorf("clear ack appends instead of replacing the thread (got %.400q…)", ack)
	}

	// Anon clears redirect to login, never 200 with an empty store claim.
	anonRec := httptest.NewRecorder()
	mux.ServeHTTP(anonRec, httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/chat/clear", nil))
	if anonRec.Code != http.StatusSeeOther {
		t.Fatalf("anon clear status = %d, want 303", anonRec.Code)
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
