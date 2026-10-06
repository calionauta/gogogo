package todo_test

// Regression guard for the ownerOf(c)=="" fail-open: every mutation
// used to proceed with an empty owner when the request carried no auth
// cookie — listing all users' todos, writing ownerless rows, and
// letting any visitor toggle or delete anyone's todo.
//
// After the RequireOwner change, all of these fail closed with a 303
// redirect to /login (same convention as handleList/handleListFragment),
// cross-user access returns 404 (ErrNotFound, never Forbidden — no
// existence leak), and no ownerless record is ever written.

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// guestClient never follows redirects and carries no cookies, so every
// response observed here is the server's first answer to an anonymous
// visitor.
func guestClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func assertRedirectToLogin(t *testing.T, resp *http.Response, what string) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("%s: anonymous status = %d, want 303", what, resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "/login") {
		t.Errorf("%s: redirect Location = %q, want /login", what, loc)
	}
}

func doAnon(t *testing.T, method, urlStr string, values url.Values) *http.Response {
	t.Helper()
	var body *strings.Reader
	if values == nil {
		body = strings.NewReader("")
	} else {
		body = strings.NewReader(values.Encode())
	}
	req, err := http.NewRequestWithContext(context.Background(), method, urlStr, body)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, urlStr, err)
	}
	if values != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := guestClient().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, urlStr, err)
	}
	return resp
}

// ownerlessCount returns how many todos rows exist without an owner.
// Must always be zero: the store layer rejects empty owners and every
// HTTP entry point requires one.
func ownerlessCount(t *testing.T, app core.App) int {
	t.Helper()
	recs, err := app.FindRecordsByFilter("todos", "", "-created", 0, 0)
	if err != nil {
		t.Fatalf("list todos: %v", err)
	}
	n := 0
	for _, r := range recs {
		if r.GetString("owner") == "" {
			n++
		}
	}
	return n
}

func firstTodoID(t *testing.T, app core.App, ownerEmail string) string {
	t.Helper()
	rec, err := app.FindAuthRecordByEmail("users", ownerEmail)
	if err != nil {
		t.Fatalf("find user %s: %v", ownerEmail, err)
	}
	recs, err := app.FindRecordsByFilter("todos", "owner = '"+rec.Id+"'", "-created", 1, 0)
	if err != nil {
		t.Fatalf("list owner todos: %v", err)
	}
	if len(recs) == 0 {
		t.Fatalf("no todos for %s", ownerEmail)
	}
	return recs[0].Id
}

func TestIntegration_AnonymousMutationsRedirectToLogin(t *testing.T) {
	t.Parallel()
	base, _, app, _, cleanup := testFixture(t)
	defer cleanup()

	// Seed one todo as the demo user so toggle/delete paths have a
	// target an anonymous visitor must NOT be able to touch.
	ctx := context.Background()
	authed := loginClient(ctx, t, base)
	mustPostCtx(ctx, t, authed, base, "/api/todos",
		url.Values{titleField: {"victim"}})
	targetID := firstTodoID(t, app, demoEmail)

	cases := map[string]struct {
		method string
		path   string
		form   url.Values
	}{
		"create":         {http.MethodPost, "/api/todos", url.Values{titleField: {"anon-write"}}},
		"toggle":         {http.MethodPost, "/api/todos/" + targetID + "/toggle", url.Values{}},
		"confirm-delete": {http.MethodGet, "/api/todos/" + targetID + "/confirm-delete", nil},
		"delete":         {http.MethodPost, "/api/todos/" + targetID + "/delete", url.Values{}},
		"clear":          {http.MethodPost, "/api/todos/completed/delete", url.Values{}},
		"fragment":       {http.MethodGet, "/api/todos/fragment", nil},
		"list":           {http.MethodGet, "/api/todos", nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			resp := doAnon(t, tc.method, base+tc.path, tc.form)
			assertRedirectToLogin(t, resp, name)
		})
	}

	// Nothing was written or removed: still exactly the victim row,
	// still uncompleted, still owned, and no ownerless rows exist.
	if n := ownerlessCount(t, app); n != 0 {
		t.Errorf("ownerless todos = %d, want 0", n)
	}
	recs, err := app.FindRecordsByFilter("todos", "", "-created", 0, 0)
	if err != nil {
		t.Fatalf("list todos: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("todos after anonymous attempts = %d, want 1", len(recs))
	}
	if recs[0].GetBool("completed") {
		t.Error("anonymous toggle flipped the victim todo")
	}
}

func TestIntegration_CrossUserMutationIsNotFound(t *testing.T) {
	t.Parallel()
	base, _, app, _, cleanup := testFixture(t)
	defer cleanup()

	// A owns a todo; B is a distinct user (seedOtherUserWithTodos sets
	// password "otherpass123").
	ctx := context.Background()
	owner := loginClient(ctx, t, base)
	mustPostCtx(ctx, t, owner, base, "/api/todos",
		url.Values{titleField: {"A-secret"}})
	if err := seedOtherUserWithTodos(app, "other@example.com", "O", 1); err != nil {
		t.Fatalf("seed other user: %v", err)
	}
	targetID := firstTodoID(t, app, demoEmail)

	jar, jarErr := cookiejar.New(nil)
	if jarErr != nil {
		t.Fatalf("cookiejar: %v", jarErr)
	}
	intruder := &http.Client{Jar: jar}
	loginUser(ctx, t, intruder, base, "other@example.com", "otherpass123")

	// B toggling or deleting A's todo must look identical to a missing
	// row: 404, never 403 (which would confirm the row exists).
	for name, tc := range map[string]struct {
		method string
		path   string
	}{
		"toggle": {http.MethodPost, "/api/todos/" + targetID + "/toggle"},
		"delete": {http.MethodPost, "/api/todos/" + targetID + "/delete"},
	} {
		t.Run(name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), tc.method,
				base+tc.path, strings.NewReader(""))
			if err != nil {
				t.Fatalf("build req: %v", err)
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			resp, err := intruder.Do(req)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("%s: cross-user status = %d, want 404", name, resp.StatusCode)
			}
		})
	}

	// A's row is untouched.
	rec, err := app.FindRecordById("todos", targetID)
	if err != nil {
		t.Fatalf("victim row gone: %v", err)
	}
	if rec.GetBool("completed") {
		t.Error("intruder toggle flipped A's todo")
	}
	if rec.GetString(titleField) != "A-secret" {
		t.Errorf("victim title = %q, want A-secret", rec.GetString(titleField))
	}
}

// TestIntegration_AnonymousStreamSeesNoTodos guards the READ half of
// the fail-open: an unauthenticated SSE stream must still open (it
// multiplexes the public demo events — queue retry feedback, LLM
// suggest, client count — that work without login), but its initial
// todo scope must be EMPTY. Before the fix the stream listed every
// user's todos unscoped.
func TestIntegration_AnonymousStreamSeesNoTodos(t *testing.T) {
	t.Parallel()
	base, _, _, _, cleanup := testFixture(t)
	defer cleanup()

	ctx := context.Background()
	authed := loginClient(ctx, t, base)
	mustPostCtx(ctx, t, authed, base, "/api/todos",
		url.Values{titleField: {"someone-private"}})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream := openSSEWithCtx(ctx, t, base, "anon-leak-probe")
	defer func() { _ = stream.Body.Close() }()

	// Collect up to ~2s of stream bytes: the initial MergeSignals (with the
	// todo list) arrives immediately; the heartbeat/client broadcasts that
	// follow carry no titles. pumpSSEUntil enforces the window even while a
	// read is parked, so this returns as soon as `itemCount` appears. The
	// previous hand-rolled loop spawned a goroutine per Read attempt (one
	// leaked per 500ms tick) to get the same guarantee.
	body := pumpSSEUntil(t, stream, 2*time.Second, func(s string) bool {
		return strings.Contains(s, `"itemCount"`)
	})

	if strings.Contains(body, "someone-private") {
		t.Errorf("anonymous stream leaked another user's todo title")
	}
	if !strings.Contains(body, `"itemCount":0`) {
		t.Errorf("anonymous stream initial itemCount != 0: %q", tailClip(body, 500))
	}
}

func tailClip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
