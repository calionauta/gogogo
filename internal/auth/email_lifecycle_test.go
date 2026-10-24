// SCOPE:core — Email verification/reset and idle-session contracts.
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

func TestEmailVerificationFlow(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	_, _, err := a.Signup(ctx, "verify@example.com", "Correct-horse-99!", "")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	a.SetRequireEmailVerification(true)
	if _, _, err := a.Login(ctx, "verify@example.com", "Correct-horse-99!"); err == nil {
		t.Fatal("unverified login accepted")
	}
	if err := a.SetAppURL("https://app.example"); err != nil {
		t.Fatalf("app url: %v", err)
	}
	var sent string
	a.SetMailer(func(_, _, body string) error { sent = body; return nil })
	mux := securityMux(a)
	rec := postJSON(t, mux, "/api/auth/verify/request", map[string]string{"email": "verify@example.com"})
	if rec.Code != http.StatusOK {
		t.Fatalf("verify request = %d (%s)", rec.Code, rec.Body.String())
	}
	link := linkFromBody(t, sent)
	token := link.Query().Get("token")
	if token == "" {
		t.Fatal("verification link has no token")
	}
	got := httptest.NewRecorder()
	mux.ServeHTTP(got, httptest.NewRequestWithContext(ctx, http.MethodGet, link.RequestURI(), nil))
	if got.Code != http.StatusSeeOther {
		t.Fatalf("verify GET = %d", got.Code)
	}
	if _, _, err := a.Login(ctx, "verify@example.com", "Correct-horse-99!"); err != nil {
		t.Fatalf("verified login: %v", err)
	}
}

func TestPasswordResetFlow(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	_, oldSession, err := a.Signup(ctx, "reset@example.com", "Correct-horse-99!", "")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	if err := a.SetAppURL("https://app.example"); err != nil {
		t.Fatalf("app url: %v", err)
	}
	var sent string
	a.SetMailer(func(_, _, body string) error { sent = body; return nil })
	a.SetPasswordResetPage(func(w http.ResponseWriter, _ *http.Request, _, _, _ string) {
		w.WriteHeader(http.StatusOK)
	})
	mux := securityMux(a)
	rec := postJSON(t, mux, "/api/auth/password/reset/request", map[string]string{"email": "reset@example.com"})
	if rec.Code != http.StatusOK {
		t.Fatalf("reset request = %d", rec.Code)
	}
	link := linkFromBody(t, sent)
	token := link.Query().Get("token")
	if token == "" {
		t.Fatal("reset link has no token")
	}
	form := httptest.NewRecorder()
	mux.ServeHTTP(form, httptest.NewRequestWithContext(ctx, http.MethodGet, link.RequestURI(), nil))
	if form.Code != http.StatusOK {
		t.Fatalf("reset form = %d", form.Code)
	}
	done := httptest.NewRecorder()
	body := url.Values{"token": {token}, "password": {"New-correct-horse-99!"}, "confirm": {"New-correct-horse-99!"}}
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/auth/password/reset",
		strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(done, req)
	if done.Code != http.StatusSeeOther {
		t.Fatalf("reset complete = %d (%s)", done.Code, done.Body.String())
	}
	if _, _, err := a.Login(ctx, "reset@example.com", "Correct-horse-99!"); err == nil {
		t.Fatal("old password still works after reset")
	}
	if _, _, err := a.Login(ctx, "reset@example.com", "New-correct-horse-99!"); err != nil {
		t.Fatalf("new password login: %v", err)
	}
	req = httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	// #nosec G124 — test-only request cookie.
	req.AddCookie(&http.Cookie{Name: CookieName, Value: oldSession})
	if _, err := a.Session(req); err == nil {
		t.Fatal("pre-reset session survived")
	}
}

func TestSessionIdleTimeout(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	_, token, err := a.Signup(ctx, "idle@example.com", "Correct-horse-99!", "")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	a.SetSessionIdle(time.Minute)
	old := time.Now().UTC().Add(-2 * time.Minute).Format(dbschema.TimeFormat)
	if err := a.q.UpdateSessionLastSeen(ctx, dbgen.UpdateSessionLastSeenParams{
		LastSeen: old, TokenHash: tokenHash(token),
	}); err != nil {
		t.Fatalf("touch: %v", err)
	}
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	// #nosec G124 — test-only request cookie.
	req.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	if _, err := a.Session(req); err == nil {
		t.Fatal("idle session accepted")
	}
}

func TestSMTPRejectsHeaderInjection(t *testing.T) {
	err := sendSMTP("127.0.0.1:1", "app@example.com", nil, "to@example.com", "hi\r\nBcc: evil", "body")
	if err == nil || !strings.Contains(err.Error(), "header injection") {
		t.Fatalf("header injection error = %v", err)
	}
}

func linkFromBody(t *testing.T, body string) *url.URL {
	t.Helper()
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "https://") || strings.HasPrefix(line, "http://") {
			u, err := url.Parse(line)
			if err != nil {
				t.Fatalf("link parse: %v", err)
			}
			return u
		}
	}
	t.Fatalf("no link in mail body: %q", body)
	return nil
}

func TestSignupWithVerificationDoesNotIssueSession(t *testing.T) {
	a := pluginAuth(t)
	a.SetRequireEmailVerification(true)
	if err := a.SetAppURL("https://app.example"); err != nil {
		t.Fatalf("app url: %v", err)
	}
	a.SetMailer(func(_, _, _ string) error { return nil })
	mux := securityMux(a)
	rec := postJSON(t, mux, "/api/auth/signup/credential", map[string]string{
		"email": "gate@example.com", "password": "Correct-horse-99!",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("signup = %d (%s)", rec.Code, rec.Body.String())
	}
	if c := sessionCookieOf(t, rec.Result().Cookies()); c != nil {
		t.Fatal("signup issued a session before verification")
	}
	if !strings.Contains(rec.Body.String(), "verification_sent") {
		t.Fatalf("signup body = %s", rec.Body.String())
	}
}

func TestResetAndVerifyRequestsAreSilentForUnknown(t *testing.T) {
	a := pluginAuth(t)
	if err := a.SetAppURL("https://app.example"); err != nil {
		t.Fatalf("app url: %v", err)
	}
	sent := 0
	a.SetMailer(func(_, _, _ string) error { sent++; return nil })
	mux := securityMux(a)
	for _, path := range []string{"/api/auth/password/reset/request", "/api/auth/verify/request"} {
		rec := postJSON(t, mux, path, map[string]string{"email": "ghost@example.com"})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s = %d, want 200", path, rec.Code)
		}
	}
	if sent != 0 {
		t.Fatalf("unknown addresses triggered %d messages", sent)
	}
}

func TestSessionListAndRevoke(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	_, first, err := a.Signup(ctx, "sessions@example.com", "Correct-horse-99!", "")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	second, _, err := a.Login(ctx, "sessions@example.com", "Correct-horse-99!")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	mux := securityMux(a)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/auth/sessions", nil)
	// #nosec G124 — test-only request cookie.
	req.AddCookie(&http.Cookie{Name: CookieName, Value: first})
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Sessions []struct {
			ID string `json:"id"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(body.Sessions))
	}
	// The newest row is the second login; revoke it.
	del := httptest.NewRecorder()
	dreq := httptest.NewRequestWithContext(ctx, http.MethodDelete,
		"/api/auth/sessions/"+body.Sessions[0].ID, nil)
	// #nosec G124 — test-only request cookie.
	dreq.AddCookie(&http.Cookie{Name: CookieName, Value: first})
	mux.ServeHTTP(del, dreq)
	if del.Code != http.StatusOK {
		t.Fatalf("revoke = %d (%s)", del.Code, del.Body.String())
	}
	for name, token := range map[string]string{"revoked": second, "kept": first} {
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
		// #nosec G124 — test-only request cookie.
		req.AddCookie(&http.Cookie{Name: CookieName, Value: token})
		_, serr := a.Session(req)
		if name == "revoked" && serr == nil {
			t.Fatal("revoked session still valid")
		}
		if name == "kept" && serr != nil {
			t.Fatalf("kept session invalid: %v", serr)
		}
	}
}

func TestSecurityEventIsPersisted(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	if _, _, err := a.Signup(ctx, "audit@example.com", "Correct-horse-99!", ""); err != nil {
		t.Fatalf("signup: %v", err)
	}
	if _, _, err := a.Login(ctx, "audit@example.com", "wrong-password"); err == nil {
		t.Fatal("bad login accepted")
	}
	rows, err := a.q.ListSecurityEvents(ctx, 10)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	found := false
	for _, row := range rows {
		if row.Event == evLoginRejected && row.Reason == reasonBadPassword {
			found = true
		}
	}
	if !found {
		t.Fatalf("rejected login absent from audit rows: %+v", rows)
	}
}
