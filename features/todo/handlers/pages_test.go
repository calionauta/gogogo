// SCOPE:feature — HTTP page contract tests (login + index, step 6).
//
// Same red-proof fixture as the data endpoints: real Turso, real auth,
// driven over HTTP. Contracts: paths, redirects, rendered components.
package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // RFC 6238 TOTP uses SHA-1; mirror production.
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/auth"
)

func TestHandlersLoginPage(t *testing.T) {
	f := newHandlersFixture(t)

	rec := f.do(http.MethodGet, "/login", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /login status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Email") {
		t.Fatal("login page missing the email field")
	}

	// A bounce from the auth guard explains itself instead of looking like a
	// page the user navigated to on purpose.
	bounced := f.do(http.MethodGet, "/login?next=%2Ftodo&auth=required", "", nil)
	if bounced.Code != http.StatusOK {
		t.Fatalf("bounced GET /login status = %d, want 200", bounced.Code)
	}
	if !strings.Contains(bounced.Body.String(), "You need to sign in to open that page.") {
		t.Fatalf("bounced login page missing the explanation (body: %s)", bounced.Body.String())
	}

	bad := f.do(http.MethodPost, "/login", "email=nobody@example.com&password=wrong-password-00", nil)
	if bad.Code != http.StatusOK {
		t.Fatalf("bad POST /login status = %d, want 200 with the error page", bad.Code)
	}
	if !strings.Contains(bad.Body.String(), "Wrong email or password") {
		t.Fatalf("bad login missing the error text (body: %s)", bad.Body.String())
	}
}

func TestHandlersLoginLogoutFlow(t *testing.T) {
	f := newHandlersFixture(t)

	// /todo bounces guests to /login.
	anon := f.do(http.MethodGet, "/todo", "", nil)
	if anon.Code != http.StatusSeeOther {
		t.Fatalf("anon /todo status = %d, want 303", anon.Code)
	}
	if loc := anon.Header().Get("Location"); loc != "/login" {
		t.Fatalf("anon /todo Location = %q, want /login", loc)
	}

	// Form login sets the cookie and redirects to /todo.
	f.do(http.MethodPost, "/api/auth/signup/credential",
		`{"email":"pages@example.com","password":"Correct-horse-99!"}`, nil)
	login := f.do(http.MethodPost, "/login", "email=pages@example.com&password=Correct-horse-99%21&next=%2Ftodo", nil)
	if login.Code != http.StatusSeeOther {
		t.Fatalf("POST /login status = %d (body: %s)", login.Code, login.Body.String())
	}
	if loc := login.Header().Get("Location"); loc != "/todo" {
		t.Fatalf("POST /login Location = %q, want /todo", loc)
	}
	var cookie *http.Cookie
	for _, c := range login.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatalf("POST /login set no %q cookie", auth.CookieName)
	}

	// The index renders for the session owner.
	f.do(http.MethodPost, "/api/todos", "title=Page+item&filter=all", cookie)
	index := f.do(http.MethodGet, "/todo", "", cookie)
	if index.Code != http.StatusOK {
		t.Fatalf("GET /todo status = %d", index.Code)
	}
	body := index.Body.String()
	if !strings.Contains(body, "pages@example.com") {
		t.Fatal("index missing the shell user email")
	}
	if !strings.Contains(body, "Page item") {
		t.Fatal("index missing the created todo")
	}

	// Logout clears the cookie and bounces the next page load.
	out := f.do(http.MethodPost, "/logout", "", cookie)
	if out.Code != http.StatusSeeOther {
		t.Fatalf("POST /logout status = %d, want 303", out.Code)
	}
	cleared := false
	for _, c := range out.Result().Cookies() {
		if c.Name == auth.CookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("logout did not expire the session cookie")
	}
	after := f.do(http.MethodGet, "/todo", "", cookie)
	if after.Code != http.StatusSeeOther {
		t.Fatalf("GET /todo after logout status = %d, want 303", after.Code)
	}
}

func TestHandlersLoginMagicAndResetForms(t *testing.T) {
	f := newHandlersFixture(t)
	if err := f.auth.SetAppURL("https://app.example"); err != nil {
		t.Fatalf("app url: %v", err)
	}
	magic := 0
	f.auth.SetMagicSender(func(_, _ string) error { magic++; return nil })
	mail := 0
	f.auth.SetMailer(func(_, _, _ string) error { mail++; return nil })
	f.signup("forms@example.com")

	rec := f.do(http.MethodPost, "/login", "intent=magic&email=forms%40example.com", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "sign-in link is on its way") {
		t.Fatalf("magic form = %d (%s)", rec.Code, rec.Body.String())
	}
	if magic != 1 {
		t.Fatalf("magic sends = %d, want 1", magic)
	}
	rec = f.do(http.MethodPost, "/login", "intent=forgot&email=forms%40example.com", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "reset link is on its way") {
		t.Fatalf("forgot form = %d (%s)", rec.Code, rec.Body.String())
	}
	if mail != 1 {
		t.Fatalf("mail sends = %d, want 1", mail)
	}
	rec = f.do(http.MethodPost, "/login", "intent=verify&email=forms%40example.com", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "verification") {
		t.Fatalf("verify form = %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestHandlersTOTPLoginForm(t *testing.T) {
	f := newHandlersFixture(t)
	if err := f.auth.SetTOTPEncKey(make([]byte, 32)); err != nil {
		t.Fatalf("totp key: %v", err)
	}
	f.signup("totp-form@example.com")
	ctx := context.Background()
	user, err := f.q.GetUserByEmail(ctx, "totp-form@example.com")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	secret, _, _, err := f.auth.BeginTOTP(ctx, user.ID)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	code := testTOTPCode(t, secret, time.Now())
	if err := f.auth.VerifyTOTPEnroll(ctx, user.ID, code); err != nil {
		t.Fatalf("enroll: %v", err)
	}

	rec := f.do(http.MethodPost, "/login", "email=totp-form%40example.com&password=Correct-horse-99%21", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Authenticator code") {
		t.Fatalf("totp step = %d (%s)", rec.Code, rec.Body.String())
	}
	code = testTOTPCode(t, secret, time.Now())
	rec = f.do(http.MethodPost, "/login",
		"intent=totp&totp_user="+user.ID+"&totp_code="+code+"&next=%2Ftodo", nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("totp verify = %d (%s)", rec.Code, rec.Body.String())
	}
	if c := sessionCookieFrom(rec); c == nil {
		t.Fatal("totp verify set no session cookie")
	}
}

func sessionCookieFrom(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return c
		}
	}
	return nil
}

// testTOTPCode mirrors internal/auth's RFC 6238 computation (SHA-1, 30s,
// 6 digits) without exporting the production helper just for a UI test.
func testTOTPCode(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	var buf [8]byte
	//nolint:gosec // TOTP step is non-negative for any post-epoch clock.
	binary.BigEndian.PutUint64(buf[:], uint64(at.Unix()/30))
	mac := hmac.New(sha1.New, raw)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[off])&0x7f)<<24 |
		uint32(sum[off+1])<<16 | uint32(sum[off+2])<<8 | uint32(sum[off+3])
	return fmt.Sprintf("%06d", code%1000000)
}
