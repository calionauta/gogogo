// The invoke endpoint, proved against the composed tree: anonymous hits
// never reach a handler, a signed-in caller runs the declared verb once
// through the same RBAC-wrapped outer handler the browser uses, and the
// owned table gains the row.
package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/auth"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
)

func signupForInvoke(t *testing.T, f *muxFixture, email string) string {
	t.Helper()
	body := `{"email":"` + email + `","password":"[LEAKGUARD_REDACTED] — Password Assignment"}`
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/auth/signup/credential", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("signup status = %d (%s)", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return c.Value
		}
	}
	t.Fatalf("signup set no %q cookie", auth.CookieName)
	return ""
}

// wireInvokeOut applies the same wrap the boot takes (applyRBAC), then
// hands the endpoint its re-serve target. Without it the endpoint answers
// 500 — like a recipe step failure, loud, not a silent skip.
func wireInvokeOut(t *testing.T, f *muxFixture) http.Handler {
	t.Helper()
	handler, err := applyRBAC(context.Background(), f.auth, f.cfg, dbgen.New(f.sqlDB), f.mux)
	if err != nil {
		t.Fatalf("applyRBAC: %v", err)
	}
	f.mux.invokeOut = handler
	return handler
}

func postInvoke(t *testing.T, f *muxFixture, token, payload string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/ext/invoke", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		// #nosec G124 — test request cookie: attributes are server-side
		// concerns; the session token is the transport here.
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token, HttpOnly: true, Secure: true})
	}
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec
}

// TestInvokeEndpointAnonymousNeverReachesAHandler is the endpoint's
// fail-loud side: no cookie, no write, 401.
func TestInvokeEndpointAnonymousNeverReachesAHandler(t *testing.T) {
	f := buildMuxFixture(t)
	wireInvokeOut(t, f)
	rec := postInvoke(t, f, "", `{"id":"todo","verb":"create","params":{"title":"Anon"}}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous invoke status = %d, want 401", rec.Code)
	}
	var n int
	if err := f.sqlDB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM ext_todo_todos WHERE title = 'Anon'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("anonymous invoke wrote %d rows", n)
	}
}

// TestInvokeEndpointRunsDeclaredVerb proves the happy path through the
// whole stack: resolve → re-serve through the RBAC-wrapped outer →
// the handler's row lands in the owned table.
func TestInvokeEndpointRunsDeclaredVerb(t *testing.T) {
	f := buildMuxFixture(t)
	wireInvokeOut(t, f)
	token := signupForInvoke(t, f, "invoker@example.com")
	rec := postInvoke(t, f, token, `{"id":"todo","verb":"create","params":{"title":"Headless todo"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("invoke status = %d (%s)", rec.Code, rec.Body.String())
	}
	var n int
	if err := f.sqlDB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM ext_todo_todos WHERE title = 'Headless todo'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("owned table has %d rows, want 1", n)
	}
}

// TestInvokeEndpointRefusesUndeclaredVerb proves the refusal direction:
// a verb the declaration never named is a 400 naming it, before any
// handler runs.
func TestInvokeEndpointRefusesUndeclaredVerb(t *testing.T) {
	f := buildMuxFixture(t)
	wireInvokeOut(t, f)
	token := signupForInvoke(t, f, "refuser@example.com")
	rec := postInvoke(t, f, token, `{"id":"todo","verb":"explode","params":{}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("undeclared verb status = %d (%s), want 400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "explode") {
		t.Fatalf("refusal must name the verb, got %q", rec.Body.String())
	}
}
