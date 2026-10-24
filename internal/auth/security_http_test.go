// SCOPE:core — Security hardening contracts: signup gate, magic-link
// origin + browser binding, OAuth browser binding, and session revocation.
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func securityMux(a *Auth) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Routes())
	return mux
}

func postJSON(
	t *testing.T, mux *http.ServeMux, path string, body any, cookies ...*http.Cookie,
) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func sessionCookieOf(t *testing.T, cookies []*http.Cookie) *http.Cookie {
	t.Helper()
	for _, c := range cookies {
		if c.Name == CookieName && c.Value != "" {
			return c
		}
	}
	return nil
}

func TestSignupGateClosesPublicCreation(t *testing.T) {
	a := pluginAuth(t)
	a.SetSignupEnabled(false)
	mux := securityMux(a)
	rec := postJSON(t, mux, "/api/auth/signup/credential", map[string]string{
		"email": "closed@example.com", "password": "Correct-horse-99!",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("signup with gate closed = %d, want 403", rec.Code)
	}
	if _, _, err := a.Signup(context.Background(), "existing@example.com", "Correct-horse-99!", ""); err != nil {
		t.Fatalf("seed existing user: %v", err)
	}
	rec = postJSON(t, mux, "/api/auth/signin/credential", map[string]string{
		"credential": "existing@example.com", "password": "Correct-horse-99!",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("existing user signin = %d, want 200", rec.Code)
	}
}

func TestPasswordTooLongRejected(t *testing.T) {
	a := pluginAuth(t)
	mux := securityMux(a)
	rec := postJSON(t, mux, "/api/auth/signup/credential", map[string]string{
		"email": "long@example.com", "password": strings.Repeat("x", maxPasswordLen+1),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized password = %d, want 400", rec.Code)
	}
}

func magicPageStub(w http.ResponseWriter, _ *http.Request, email, token, bind, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	// #nosec G705 — test-only plain-text probe, not an HTML response.
	_, _ = w.Write([]byte(email + "|" + token + "|" + bind + "|" + errMsg))
}

func TestMagicLinkUsesConfiguredOriginAndStrictBinding(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	if _, _, err := a.Signup(ctx, "magic@example.com", "Correct-horse-99!", ""); err != nil {
		t.Fatalf("signup: %v", err)
	}
	if err := a.SetAppURL("https://app.example"); err != nil {
		t.Fatalf("set app url: %v", err)
	}
	a.SetMagicLinkBindBrowser(true)
	a.SetMagicConfirmPage(magicPageStub)
	var sent string
	a.SetMagicSender(func(_, link string) error { sent = link; return nil })
	mux := securityMux(a)

	rec := postJSON(t, mux, "/api/auth/magic/request", map[string]string{"email": "magic@example.com"})
	if rec.Code != http.StatusOK {
		t.Fatalf("request = %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.HasPrefix(sent, "https://app.example/api/auth/magic?") {
		t.Fatalf("link did not use APP_URL: %q", sent)
	}
	if strings.Contains(sent, "example.com") && !strings.Contains(sent, "app.example") {
		t.Fatalf("request Host leaked into the link: %q", sent)
	}
	bind := bindCookie(rec.Result().Cookies(), magicBindCookie)
	if bind == nil {
		t.Fatal("strict mode set no browser-binding cookie")
	}
	u, err := url.Parse(sent)
	if err != nil {
		t.Fatalf("link parse: %v", err)
	}
	if u.Query().Get("bind") != bind.Value {
		t.Fatalf("link bind does not match cookie")
	}

	// GET is read-only for a foreign browser: it shows the error page, does
	// not consume the token, and sets no session.
	noCookie := httptest.NewRecorder()
	mux.ServeHTTP(noCookie, httptest.NewRequestWithContext(ctx, http.MethodGet, u.RequestURI(), nil))
	if noCookie.Code != http.StatusOK || !strings.Contains(noCookie.Body.String(), "Open this link") {
		t.Fatalf("foreign browser GET = %d (%s)", noCookie.Code, noCookie.Body.String())
	}
	if c := sessionCookieOf(t, noCookie.Result().Cookies()); c != nil {
		t.Fatal("foreign browser received a session cookie")
	}

	// The requesting browser confirms with a POST.
	ok := httptest.NewRecorder()
	form := url.Values{"token": {u.Query().Get("token")}, "bind": {bind.Value}}
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/auth/magic",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(bind)
	mux.ServeHTTP(ok, req)
	if ok.Code != http.StatusSeeOther {
		t.Fatalf("bound browser = %d (%s)", ok.Code, ok.Body.String())
	}
	if c := sessionCookieOf(t, ok.Result().Cookies()); c == nil {
		t.Fatal("bound browser received no session cookie")
	}
}

func TestMagicLinkCrossDeviceConfirmation(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	if _, _, err := a.Signup(ctx, "cross@example.com", "Correct-horse-99!", ""); err != nil {
		t.Fatalf("signup: %v", err)
	}
	if err := a.SetAppURL("https://app.example"); err != nil {
		t.Fatalf("set app url: %v", err)
	}
	a.SetMagicConfirmPage(magicPageStub)
	var sent string
	a.SetMagicSender(func(_, link string) error { sent = link; return nil })
	mux := securityMux(a)

	rec := postJSON(t, mux, "/api/auth/magic/request", map[string]string{"email": "cross@example.com"})
	if rec.Code != http.StatusOK {
		t.Fatalf("request = %d", rec.Code)
	}
	u, err := url.Parse(sent)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if u.Query().Get("bind") != "" {
		t.Fatal("default cross-device link unexpectedly carries a bind")
	}

	// A different browser opens the link: GET only renders the confirmation.
	page := httptest.NewRecorder()
	mux.ServeHTTP(page, httptest.NewRequestWithContext(ctx, http.MethodGet, u.RequestURI(), nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "cross@example.com") {
		t.Fatalf("confirmation GET = %d (%s)", page.Code, page.Body.String())
	}
	if c := sessionCookieOf(t, page.Result().Cookies()); c != nil {
		t.Fatal("GET opened a session")
	}

	// The same other browser confirms: cross-device works.
	ok := httptest.NewRecorder()
	form := url.Values{"token": {u.Query().Get("token")}}
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/auth/magic",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(ok, req)
	if ok.Code != http.StatusSeeOther {
		t.Fatalf("confirmation POST = %d (%s)", ok.Code, ok.Body.String())
	}
	if c := sessionCookieOf(t, ok.Result().Cookies()); c == nil {
		t.Fatal("confirmation POST opened no session")
	}

	// Consumed: a replay is just a redirect.
	replay := httptest.NewRecorder()
	mux.ServeHTTP(replay, httptest.NewRequestWithContext(ctx, http.MethodGet, u.RequestURI(), nil))
	if replay.Code != http.StatusSeeOther {
		t.Fatalf("replay GET = %d, want redirect", replay.Code)
	}
}

func TestMagicLinkWithoutAppURLFailsClosed(t *testing.T) {
	a := pluginAuth(t)
	mux := securityMux(a)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/api/auth/magic/request", strings.NewReader(`{"email":"any@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "evil.example"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("magic request without APP_URL = %d, want 503", rec.Code)
	}
}

func TestOAuthCallbackRequiresBrowserBinding(t *testing.T) {
	a := pluginAuth(t)
	idp := stubIDP(t, "bind@example.com", true)
	a.SetOAuthProviders(map[string]OAuthProvider{"stub": subProvider(idp.URL)})
	mux := securityMux(a)
	start := httptest.NewRecorder()
	mux.ServeHTTP(start, httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/auth/oauth/stub", nil))
	loc, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatalf("start location: %v", err)
	}
	bind := bindCookie(start.Result().Cookies(), oauthBindCookie)
	if bind == nil {
		t.Fatal("OAuth start set no binding cookie")
	}

	foreign := httptest.NewRecorder()
	mux.ServeHTTP(foreign, httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/auth/oauth/stub/callback?code=stub-code&state="+loc.Query().Get("state"), nil))
	if foreign.Code != http.StatusBadRequest {
		t.Fatalf("callback without cookie = %d, want 400", foreign.Code)
	}

	ok := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/auth/oauth/stub/callback?code=stub-code&state="+loc.Query().Get("state"), nil)
	req.AddCookie(bind)
	mux.ServeHTTP(ok, req)
	if ok.Code != http.StatusSeeOther {
		t.Fatalf("bound callback = %d (%s)", ok.Code, ok.Body.String())
	}
}

func TestPasswordChangeRevokesOtherSessions(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	if _, _, err := a.Signup(ctx, "rotate@example.com", "Correct-horse-99!", ""); err != nil {
		t.Fatalf("signup: %v", err)
	}
	first, _, err := a.Login(ctx, "rotate@example.com", "Correct-horse-99!")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	second, _, err := a.Login(ctx, "rotate@example.com", "Correct-horse-99!")
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	mux := securityMux(a)
	rec := postJSON(t, mux, "/api/auth/password", map[string]string{
		"currentPassword": "Correct-horse-99!",
		"newPassword":     "New-correct-horse-99!",
		// #nosec G124 — test-only request cookie.
	}, &http.Cookie{Name: CookieName, Value: first})
	if rec.Code != http.StatusOK {
		t.Fatalf("password change = %d (%s)", rec.Code, rec.Body.String())
	}
	fresh := sessionCookieOf(t, rec.Result().Cookies())
	if fresh == nil {
		t.Fatal("password change issued no replacement session")
	}
	for name, token := range map[string]string{"old": second, "rotated": first} {
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
		// #nosec G124 — test-only request cookie.
		req.AddCookie(&http.Cookie{Name: CookieName, Value: token})
		if _, err := a.Session(req); err == nil {
			t.Fatalf("%s session survived credential rotation", name)
		}
	}
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	req.AddCookie(fresh)
	if _, err := a.Session(req); err != nil {
		t.Fatalf("replacement session invalid: %v", err)
	}
}

func TestRevokeAllSessions(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	_, token, err := a.Signup(ctx, "revoke@example.com", "Correct-horse-99!", "")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	mux := securityMux(a)
	// #nosec G124 — test-only request cookie.
	cookie := &http.Cookie{Name: CookieName, Value: token}
	bad := postJSON(t, mux, "/api/auth/sessions/revoke-all",
		map[string]string{"password": "wrong-password"}, cookie)
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("wrong-password revoke-all = %d, want 401", bad.Code)
	}
	rec := postJSON(t, mux, "/api/auth/sessions/revoke-all",
		map[string]string{"password": "Correct-horse-99!"}, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke-all = %d (%s)", rec.Code, rec.Body.String())
	}
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	// #nosec G124 — test-only request cookie.
	req.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	if _, err := a.Session(req); err == nil {
		t.Fatal("revoked session still valid")
	}
}

// TestMagicRequestWithoutSenderIsUniform pins the anti-enumeration contract
// when delivery is not wired: a known and an unknown address must receive
// the same answer (503), never 500-vs-200.
func TestMagicRequestWithoutSenderIsUniform(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	if _, _, err := a.Signup(ctx, "known@example.com", "Correct-horse-99!", ""); err != nil {
		t.Fatalf("signup: %v", err)
	}
	if err := a.SetAppURL("https://app.example"); err != nil {
		t.Fatalf("set app url: %v", err)
	}
	mux := securityMux(a)
	for _, email := range []string{"known@example.com", "ghost@example.com"} {
		rec := postJSON(t, mux, "/api/auth/magic/request", map[string]string{"email": email})
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s = %d, want 503", email, rec.Code)
		}
	}
}

// TestSignupThrottleBoundsArgonWork pins the signup budget: each accepted
// attempt can cost a 128 MiB hash, so the eleventh request in the window is
// refused before parsing or hashing. Bodies are invalid on purpose — the
// throttle must fire before the expensive path.
func TestSignupThrottleBoundsArgonWork(t *testing.T) {
	a := pluginAuth(t)
	mux := securityMux(a)
	for i := range signupAttemptMax {
		rec := postJSON(t, mux, "/api/auth/signup/credential", map[string]string{})
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d throttled before the budget", i+1)
		}
	}
	rec := postJSON(t, mux, "/api/auth/signup/credential", map[string]string{})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request %d = %d, want 429", signupAttemptMax+1, rec.Code)
	}
}

// TestRevokeAllSessionsDropsPendingMagicLink pins the credential boundary:
// a login link issued before a revoked session must not open a new one.
func TestRevokeAllSessionsDropsPendingMagicLink(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	if _, _, err := a.Signup(ctx, "magic-revoke@example.com", "Correct-horse-99!", ""); err != nil {
		t.Fatalf("signup: %v", err)
	}
	var sent string
	a.SetMagicSender(func(_, link string) error { sent = link; return nil })
	if err := a.RequestMagicLink(ctx, "magic-revoke@example.com", "https://app.example", "", a.magicSender()); err != nil {
		t.Fatalf("request: %v", err)
	}
	u, err := url.Parse(sent)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	token := u.Query().Get("token")
	if token == "" {
		t.Fatal("no token in link")
	}
	row, err := a.q.GetUserByEmail(ctx, "magic-revoke@example.com")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if err := a.RevokeAllSessions(ctx, row.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := a.VerifyMagicLink(ctx, token); err == nil {
		t.Fatal("pending magic link survived revoke-all")
	}
}

func TestSetAppURLRequiresHTTPSInProduction(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	if _, _, err := a.Signup(ctx, "prod-url@example.com", "Correct-horse-99!", ""); err != nil {
		t.Fatalf("signup: %v", err)
	}
	secure, err := New(a.q, true)
	if err != nil {
		t.Fatalf("secure auth: %v", err)
	}
	if err := secure.SetAppURL("http://app.example"); err == nil {
		t.Fatal("http APP_URL accepted in production")
	}
	if err := secure.SetAppURL("https://app.example"); err != nil {
		t.Fatalf("https APP_URL rejected: %v", err)
	}
	if err := secure.SetAppURL("http://127.0.0.1:3000"); err != nil {
		t.Fatalf("loopback http rejected: %v", err)
	}
}

func TestMagicRequestThrottlePerIP(t *testing.T) {
	a := pluginAuth(t)
	a.SetMagicSender(func(_, _ string) error { return nil })
	if err := a.SetAppURL("https://app.example"); err != nil {
		t.Fatalf("set app url: %v", err)
	}
	mux := securityMux(a)
	for i := range magicAttemptMax {
		rec := postJSON(t, mux, "/api/auth/magic/request", map[string]string{"email": "ghost@example.com"})
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d = %d, want 200", i+1, rec.Code)
		}
	}
	rec := postJSON(t, mux, "/api/auth/magic/request", map[string]string{"email": "ghost@example.com"})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request %d = %d, want 429", magicAttemptMax+1, rec.Code)
	}
}

func TestOAuthStartThrottlePerIP(t *testing.T) {
	a := pluginAuth(t)
	idp := stubIDP(t, "throttle@example.com", true)
	a.SetOAuthProviders(map[string]OAuthProvider{"stub": subProvider(idp.URL)})
	mux := securityMux(a)
	for i := range oauthStartAttemptMax {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet,
			"/api/auth/oauth/stub", nil))
		if rec.Code != http.StatusFound {
			t.Fatalf("start %d = %d, want 302", i+1, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/auth/oauth/stub", nil))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("start %d = %d, want 429", oauthStartAttemptMax+1, rec.Code)
	}
}
