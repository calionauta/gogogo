// SCOPE:core — Auth plugin tests (TOTP vectors, magic-link, OAuth stub).
package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// rfcSecret is the RFC 6238 Appendix B SHA-1 secret ("12345678901234567890").
// #nosec G101 — a published test vector, not a credential.
const rfcSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

// TestTOTPVectors pins the implementation to RFC 6238 Appendix B: the
// 8-digit reference codes truncated to our 6-digit emission.
func TestTOTPVectors(t *testing.T) {
	for _, tc := range []struct {
		unix int64
		want string
	}{
		{59, "287082"},          // 94287082
		{1111111109, "081804"},  // 07081804
		{1111111111, "050471"},  // 14050471
		{1234567890, "005924"},  // 89005924
		{2000000000, "279037"},  // 69279037
		{20000000000, "353130"}, // 65353130
	} {
		got, err := totpCode(rfcSecret, time.Unix(tc.unix, 0).UTC())
		if err != nil {
			t.Fatalf("t=%d: %v", tc.unix, err)
		}
		if got != tc.want {
			t.Errorf("t=%d: code = %q, want %q", tc.unix, got, tc.want)
		}
	}
	if verifyTOTP(rfcSecret, "000000", time.Unix(59, 0)) {
		t.Error("bogus code accepted")
	}
	if !verifyTOTP(rfcSecret, "287082", time.Unix(59, 0)) {
		t.Error("valid code rejected")
	}
}

func pluginAuth(t *testing.T) *Auth {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/plugins.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("db migrate: %v", merr)
	}
	a, err := New(dbgen.New(sqlDB), false)
	if err != nil {
		t.Fatalf("auth new: %v", err)
	}
	// Production always wires a seal key (AUTH_ENC_KEY or the auto-created
	// file); the fixture mirrors that so enrollment paths behave as shipped.
	k := testKey(t)
	if err := a.SetTOTPEncKey(k[:]); err != nil {
		t.Fatalf("seal key: %v", err)
	}
	return a
}

func TestTOTPEnrollAndLogin(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)

	user, _, serr := a.Signup(ctx, "totp@example.com", "Correct-horse-99!", "")
	if serr != nil {
		t.Fatalf("signup: %v", serr)
	}

	// Before enrollment, password login opens sessions directly.
	if _, required, lerr := a.Login(ctx, "totp@example.com", "Correct-horse-99!"); lerr != nil || required {
		t.Fatalf("pre-enroll login = %v, required=%v; want token, false", lerr, required)
	}

	secret, uri, _, berr := a.BeginTOTP(ctx, user.ID)
	if berr != nil {
		t.Fatalf("begin: %v", berr)
	}
	if !strings.HasPrefix(uri, "otpauth://totp/") {
		t.Fatalf("provisioning uri = %q, want otpauth://", uri)
	}
	// Abandoned enrollment enforces nothing yet.
	if _, required, uerr := a.Login(ctx, "totp@example.com", "Correct-horse-99!"); uerr != nil || required {
		t.Fatalf("unverified login = %v, required=%v; want token, false", uerr, required)
	}

	code, cerr := totpCode(secret, time.Now())
	if cerr != nil {
		t.Fatalf("code: %v", cerr)
	}
	if verr := a.VerifyTOTPEnroll(ctx, user.ID, "000000"); verr == nil {
		t.Fatal("bad enroll code accepted")
	}
	if verr := a.VerifyTOTPEnroll(ctx, user.ID, code); verr != nil {
		t.Fatalf("enroll: %v", verr)
	}

	// Enforced from here: password alone opens nothing.
	token, required, eerr := a.Login(ctx, "totp@example.com", "Correct-horse-99!")
	if eerr != nil || !required || token != "" {
		t.Fatalf("enforced login = %q, %v, %v; want empty, true, nil", token, required, eerr)
	}
	if _, err := a.VerifyTOTPLogin(ctx, user.ID, "000000"); err == nil {
		t.Fatal("bad login code accepted")
	}
	code, _ = totpCode(secret, time.Now())
	token, err := a.VerifyTOTPLogin(ctx, user.ID, code)
	if err != nil {
		t.Fatalf("totp login: %v", err)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	// #nosec G124 — test-only request cookie; never traverses a network.
	req.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	if _, err := a.Session(req); err != nil {
		t.Fatalf("session after totp login: %v", err)
	}

	// Disable (fresh password proof inside the route; direct here) drops
	// enforcement back to password-only.
	if err := a.DisableTOTP(ctx, user.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, required, err := a.Login(ctx, "totp@example.com", "Correct-horse-99!"); err != nil || required {
		t.Fatalf("post-disable login = %v, required=%v", err, required)
	}
}

func TestMagicLinkFlow(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	if _, _, err := a.Signup(ctx, "magic@example.com", "Correct-horse-99!", ""); err != nil {
		t.Fatalf("signup: %v", err)
	}
	var sentTo, sentLink string
	a.SetMagicSender(func(to, link string) error {
		sentTo, sentLink = to, link
		return nil
	})

	if err := a.RequestMagicLink(ctx, "magic@example.com", "http://x.test", a.magicSender()); err != nil {
		t.Fatalf("request: %v", err)
	}
	if sentTo != "magic@example.com" {
		t.Fatalf("sent to %q", sentTo)
	}
	u, err := url.Parse(sentLink)
	if err != nil {
		t.Fatalf("link parse: %v", err)
	}
	token := u.Query().Get("token")
	if token == "" {
		t.Fatalf("link carries no token: %q", sentLink)
	}

	sessionToken, err := a.VerifyMagicLink(ctx, token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	// #nosec G124 — test-only request cookie.
	req.AddCookie(&http.Cookie{Name: CookieName, Value: sessionToken})
	if _, err := a.Session(req); err != nil {
		t.Fatalf("session after magic: %v", err)
	}

	// Single-use: replay fails.
	if _, err := a.VerifyMagicLink(ctx, token); err == nil {
		t.Fatal("replayed magic token accepted")
	}

	// Unknown addresses: silent nil, nothing sent.
	sentTo = ""
	if err := a.RequestMagicLink(ctx, "ghost@example.com", "http://x.test", func(to, _ string) error {
		sentTo = to
		return nil
	}); err != nil {
		t.Fatalf("ghost request err = %v, want nil", err)
	}
	if sentTo != "" {
		t.Fatal("ghost request sent mail")
	}

	// No sender configured: fail closed, not silently dropped.
	a.SetMagicSender(nil)
	if err := a.RequestMagicLink(ctx, "magic@example.com", "http://x.test", a.magicSender()); err == nil {
		t.Fatal("senderless request succeeded")
	}
}

// stubIdP serves token exchange + userinfo for OAuth tests.
func stubIDP(t *testing.T, email string, verified bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"stub-access","token_type":"bearer","expires_in":3600}`))
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer stub-access" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if verified {
			_, _ = w.Write([]byte(`{"sub":"stub-sub-1","email":"` + email + `","email_verified":true}`))
		} else {
			_, _ = w.Write([]byte(`{"sub":"stub-sub-1","email":"` + email + `","email_verified":false}`))
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestOAuthFlow(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	idp := stubIDP(t, "oauth@example.com", true)
	a.SetOAuthProviders(map[string]OAuthProvider{
		"stub": {
			Name: "stub", ClientID: "id", ClientSecret: "secret",
			RedirectURL:  "http://x.test/api/auth/oauth/stub/callback",
			AuthURL:      idp.URL + "/auth",
			TokenURL:     idp.URL + "/token",
			UserInfoURL:  idp.URL + "/userinfo",
			Scopes:       []string{"email"},
			ParseEmail:   parseGoogleEmail,
			ParseSubject: parseGoogleSubject,
		},
	})
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Routes())

	// Unknown provider 404s.
	rec404 := httptest.NewRecorder()
	mux.ServeHTTP(rec404, httptest.NewRequestWithContext(context.Background(),
		http.MethodGet, "/api/auth/oauth/nope", nil))
	if rec404.Code != http.StatusNotFound {
		t.Fatalf("unknown provider status = %d, want 404", rec404.Code)
	}

	// Start redirects with a state.
	startRec := httptest.NewRecorder()
	mux.ServeHTTP(startRec, httptest.NewRequestWithContext(context.Background(),
		http.MethodGet, "/api/auth/oauth/stub", nil))
	if startRec.Code != http.StatusFound {
		t.Fatalf("start status = %d, want 302", startRec.Code)
	}
	loc, err := url.Parse(startRec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("start location: %v", err)
	}
	state := loc.Query().Get("state")
	if state == "" {
		t.Fatal("start redirect carries no state")
	}

	// Callback completes: user provisioned + linked, cookie set.
	cbRec := httptest.NewRecorder()
	mux.ServeHTTP(cbRec, httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/auth/oauth/stub/callback?code=stub-code&state="+state, nil))
	if cbRec.Code != http.StatusSeeOther {
		t.Fatalf("callback status = %d (body: %s)", cbRec.Code, cbRec.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range cbRec.Result().Cookies() {
		if c.Name == CookieName && c.Value != "" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("callback set no session cookie")
	}

	// Replaying the consumed state fails.
	replayRec := httptest.NewRecorder()
	mux.ServeHTTP(replayRec, httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/auth/oauth/stub/callback?code=stub-code&state="+state, nil))
	if replayRec.Code != http.StatusBadRequest {
		t.Fatalf("replay status = %d, want 400", replayRec.Code)
	}

	// Second login links instead of duplicating: still exactly one user.
	start2 := httptest.NewRecorder()
	mux.ServeHTTP(start2, httptest.NewRequestWithContext(context.Background(),
		http.MethodGet, "/api/auth/oauth/stub", nil))
	state2, _ := url.Parse(start2.Header().Get("Location"))
	cb2 := httptest.NewRecorder()
	mux.ServeHTTP(cb2, httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/auth/oauth/stub/callback?code=stub-code&state="+state2.Query().Get("state"), nil))
	if cb2.Code != http.StatusSeeOther {
		t.Fatalf("second callback status = %d", cb2.Code)
	}
	n, err := a.q.CountUsers(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("users = %d, want 1 (link, not duplicate)", n)
	}
}

func TestPasswordVersionMarker(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)

	if _, _, err := a.Signup(ctx, "ver@example.com", "Correct-horse-99!", ""); err != nil {
		t.Fatalf("signup: %v", err)
	}
	row, err := a.q.GetUserByEmail(ctx, "ver@example.com")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if !strings.HasPrefix(row.PasswordHash, "argon2id$v=1$") {
		t.Fatalf("hash = %q, want versioned argon2id$v=1$", row.PasswordHash)
	}
	// Unversioned (pre-marker) hashes fail closed, never verify.
	legacy := strings.Replace(row.PasswordHash, "argon2id$v=1$", "argon2id$", 1)
	if ok, _ := verifyPassword(legacy, "Correct-horse-99!"); ok {
		t.Fatal("unversioned hash verified")
	}
}

func TestMagicLinkCooldown(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	if _, _, err := a.Signup(ctx, "cool@example.com", "Correct-horse-99!", ""); err != nil {
		t.Fatalf("signup: %v", err)
	}
	var sends int
	send := func(_, _ string) error {
		sends++
		return nil
	}
	if err := a.RequestMagicLink(ctx, "cool@example.com", "http://x.test", send); err != nil {
		t.Fatalf("first request: %v", err)
	}
	// Immediate second request: silent nil, no second mail (cooldown).
	if err := a.RequestMagicLink(ctx, "cool@example.com", "http://x.test", send); err != nil {
		t.Fatalf("cooldown request err = %v, want silent nil", err)
	}
	if sends != 1 {
		t.Fatalf("sends = %d, want 1 (cooldown suppresses the second)", sends)
	}
}

func TestRecoveryCodes(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	user, _, err := a.Signup(ctx, "rec@example.com", "Correct-horse-99!", "")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	secret, _, err := func() (string, string, error) {
		s, u, _, e := a.BeginTOTP(ctx, user.ID)
		return s, u, e
	}()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	code, _ := totpCode(secret, time.Now())
	if verr := a.VerifyTOTPEnroll(ctx, user.ID, code); verr != nil {
		t.Fatalf("enroll: %v", verr)
	}

	codes, err := a.RotateRecoveryCodes(ctx, user.ID)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if len(codes) != 10 {
		t.Fatalf("codes = %d, want 10", len(codes))
	}
	n, err := a.q.CountRecoveryCodes(ctx, user.ID)
	if err != nil || n != 10 {
		t.Fatalf("stored = %d, %v; want 10 hashes", n, err)
	}

	// A recovery code completes the pending login…
	if _, err := a.VerifyTOTPLogin(ctx, user.ID, codes[3]); err != nil {
		t.Fatalf("recovery login: %v", err)
	}
	// …exactly once.
	if _, err := a.VerifyTOTPLogin(ctx, user.ID, codes[3]); err == nil {
		t.Fatal("reused recovery code accepted")
	}
	// Wrong codes still fail, and rotation kills the old batch.
	if _, err := a.VerifyTOTPLogin(ctx, user.ID, "XXXX-XXXX-XXXX-XXXX"); err == nil {
		t.Fatal("bogus recovery code accepted")
	}
	if _, err := a.RotateRecoveryCodes(ctx, user.ID); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if _, err := a.VerifyTOTPLogin(ctx, user.ID, codes[4]); err == nil {
		t.Fatal("rotated-out recovery code accepted")
	}
}

func TestTOTPThrottle(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	user, _, err := a.Signup(ctx, "throttle@example.com", "Correct-horse-99!", "")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	secret, _, err := func() (string, string, error) {
		s, u, _, e := a.BeginTOTP(ctx, user.ID)
		return s, u, e
	}()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	code, _ := totpCode(secret, time.Now())
	if err := a.VerifyTOTPEnroll(ctx, user.ID, code); err != nil {
		t.Fatalf("enroll: %v", err)
	}

	// Mechanism, white-box: 10 failures fit the burst, the 11th does not,
	// and success clears the slate (no lockout from others' guessing).
	for i := range 10 {
		if !a.totpAttempts.allow(user.ID) {
			t.Fatalf("attempt %d refused inside the burst", i+1)
		}
	}
	if a.totpAttempts.allow(user.ID) {
		t.Fatal("11th attempt allowed past the burst")
	}
	a.totpAttempts.forget(user.ID)
	if !a.totpAttempts.allow(user.ID) {
		t.Fatal("attempt refused after forget")
	}
	a.totpAttempts.forget(user.ID)

	// Behavior: ten wrong codes, then the RIGHT code still opens a
	// session — throttling bounds guessing without locking the owner out.
	for range 10 {
		_, _ = a.VerifyTOTPLogin(ctx, user.ID, "000000")
	}
	code, _ = totpCode(secret, time.Now())
	if _, err := a.VerifyTOTPLogin(ctx, user.ID, code); err != nil {
		t.Fatalf("right code after burst: %v", err)
	}
}

func TestOAuthPKCEChallenge(t *testing.T) {
	a := pluginAuth(t)
	idp := stubIDP(t, "pkce@example.com", true)
	a.SetOAuthProviders(map[string]OAuthProvider{
		"stub": {
			Name: "stub", ClientID: "id", ClientSecret: "secret",
			RedirectURL:  "http://x.test/api/auth/oauth/stub/callback",
			AuthURL:      idp.URL + "/auth",
			TokenURL:     idp.URL + "/token",
			UserInfoURL:  idp.URL + "/userinfo",
			Scopes:       []string{"email"},
			ParseEmail:   parseGoogleEmail,
			ParseSubject: parseGoogleSubject,
		},
	})
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Routes())

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(),
		http.MethodGet, "/api/auth/oauth/stub", nil))
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("location: %v", err)
	}
	q := loc.Query()
	if q.Get("code_challenge") == "" {
		t.Fatal("start redirect missing PKCE code_challenge")
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Fatalf("challenge method = %q, want S256", q.Get("code_challenge_method"))
	}
}
