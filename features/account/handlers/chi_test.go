// SCOPE:feature — /account security page contracts.
package handlers

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 TOTP uses SHA-1.
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/license"
)

func accountFixture(t *testing.T) (*auth.Auth, *dbgen.Queries, *http.ServeMux) {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/account.db")
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("migrate: %v", merr)
	}
	q := dbgen.New(sqlDB)
	a, aerr := auth.New(q, false)
	if aerr != nil {
		t.Fatalf("auth: %v", aerr)
	}
	cfg := &config.Config{AppName: "gogogo", BuildLabel: "test"}
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Routes())
	mux.Handle("/", NewHandler(cfg, a, q).Routes())
	return a, q, mux
}

func accountDo(mux *http.ServeMux, method, target, form, token string) *httptest.ResponseRecorder {
	var body *strings.Reader
	if form != "" {
		body = strings.NewReader(form)
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequestWithContext(context.Background(), method, target, body)
	if form != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if token != "" {
		// #nosec G124 — test-only request cookie.
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestAccountPageAndPasswordChange(t *testing.T) {
	a, _, mux := accountFixture(t)
	_, old, err := a.Signup(context.Background(), "acct@example.com", "Correct-horse-99!", "")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	page := accountDo(mux, http.MethodGet, "/account", "", old)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "acct@example.com") {
		t.Fatalf("account page = %d (%s)", page.Code, page.Body.String())
	}
	rec := accountDo(mux, http.MethodPost, "/account/password",
		"current=Correct-horse-99%21&new=New-correct-horse-99%21&confirm=New-correct-horse-99%21", old)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("password change = %d (%s)", rec.Code, rec.Body.String())
	}
	var fresh string
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			fresh = c.Value
		}
	}
	if fresh == "" {
		t.Fatal("password change issued no replacement cookie")
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	// #nosec G124 — test-only request cookie.
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: old})
	if _, err := a.Session(req); err == nil {
		t.Fatal("old session survived password change")
	}
	req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	// #nosec G124 — test-only request cookie.
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: fresh})
	if _, err := a.Session(req); err != nil {
		t.Fatalf("replacement session invalid: %v", err)
	}
}

func TestAccountTOTPEnrollAndDisable(t *testing.T) {
	a, q, mux := accountFixture(t)
	if err := a.SetTOTPEncKey(make([]byte, 32)); err != nil {
		t.Fatalf("key: %v", err)
	}
	user, token, err := a.Signup(context.Background(), "otp-account@example.com", "Correct-horse-99!", "")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	secret, _, _, err := a.BeginTOTP(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if verr := a.VerifyTOTPEnroll(context.Background(), user.ID, testTOTP(secret)); verr != nil {
		t.Fatalf("enroll: %v", verr)
	}
	page := accountDo(mux, http.MethodGet, "/account", "", token)
	if !strings.Contains(page.Body.String(), "enabled") {
		t.Fatalf("account page missing TOTP enabled state")
	}
	rec := accountDo(mux, http.MethodPost, "/account/totp/disable",
		"password=Correct-horse-99%21", token)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("disable = %d (%s)", rec.Code, rec.Body.String())
	}
	row, err := q.GetUser(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	if row.TotpVerified != 0 {
		t.Fatal("TOTP still enabled after disable")
	}
}

func TestAccountSessionsRevoke(t *testing.T) {
	a, _, mux := accountFixture(t)
	_, first, err := a.Signup(context.Background(), "sess-acct@example.com", "Correct-horse-99!", "")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	second, _, err := a.Login(context.Background(), "sess-acct@example.com", "Correct-horse-99!")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	page := accountDo(mux, http.MethodGet, "/account", "", first)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Sessions") {
		t.Fatalf("sessions page = %d", page.Code)
	}
	// The newest session appears first in the page; revoke both through the
	// form endpoint by reading the ids from the rendered page is brittle.
	// Revoke-all is the deterministic page contract.
	rec := accountDo(mux, http.MethodPost, "/account/sessions/revoke-all", "password=Correct-horse-99%21", first)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("revoke-all = %d", rec.Code)
	}
	for name, token := range map[string]string{"first": first, "second": second} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
		// #nosec G124 — test-only request cookie.
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		if _, err := a.Session(req); err == nil {
			t.Fatalf("%s session survived revoke-all", name)
		}
	}
}

func testTOTP(secret string) string {
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return ""
	}
	var buf [8]byte
	//nolint:gosec // TOTP step is non-negative for any post-epoch clock.
	binary.BigEndian.PutUint64(buf[:], uint64(time.Now().Unix()/30))
	mac := hmac.New(sha1.New, raw)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[off])&0x7f)<<24 | uint32(sum[off+1])<<16 |
		uint32(sum[off+2])<<8 | uint32(sum[off+3])
	return fmt.Sprintf("%06d", code%1000000)
}

func TestAccountRevokeAllRequiresPassword(t *testing.T) {
	a, q, mux := accountFixture(t)
	ctx := context.Background()
	_, first, err := a.Signup(ctx, "sess-proof@example.com", "Correct-horse-99!", "")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	second, _, err := a.Login(ctx, "sess-proof@example.com", "Correct-horse-99!")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	rec := accountDo(mux, http.MethodPost, "/account/sessions/revoke-all", "password=wrong-password", first)
	if rec.Code != http.StatusOK {
		t.Fatalf("wrong-password revoke = %d, want the rendered error page", rec.Code)
	}
	for name, token := range map[string]string{"first": first, "second": second} {
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
		// #nosec G124 — test-only request cookie.
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		if _, serr := a.Session(req); serr != nil {
			t.Fatalf("%s session revoked without the password: %v", name, serr)
		}
	}
	rows, err := q.ListSecurityEvents(ctx, 50)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	for _, row := range rows {
		if row.Event == auth.EventStepUpRejected && row.Reason == "revoke all" {
			return
		}
	}
	t.Fatalf("failed step-up absent from audit rows: %+v", rows)
}

func TestAccountRecoveryCodesRequirePassword(t *testing.T) {
	a, q, mux := accountFixture(t)
	if err := a.SetTOTPEncKey(make([]byte, 32)); err != nil {
		t.Fatalf("key: %v", err)
	}
	ctx := context.Background()
	user, token, err := a.Signup(ctx, "recovery-acct@example.com", "Correct-horse-99!", "")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	if _, _, lerr := a.Login(ctx, "recovery-acct@example.com", "Correct-horse-99!"); lerr != nil {
		t.Fatalf("login: %v", lerr)
	}
	secret, _, _, err := a.BeginTOTP(ctx, user.ID)
	if err != nil {
		t.Fatalf("begin totp: %v", err)
	}
	if verr := a.VerifyTOTPEnroll(ctx, user.ID, testTOTP(secret)); verr != nil {
		t.Fatalf("enroll: %v", verr)
	}
	before, err := q.CountRecoveryCodes(ctx, user.ID)
	if err != nil {
		t.Fatalf("count before: %v", err)
	}
	bad := accountDo(mux, http.MethodPost, "/account/totp/recovery", "password=wrong", token)
	if bad.Code != http.StatusOK || !strings.Contains(bad.Body.String(), "Password is wrong.") {
		t.Fatalf("wrong-password recovery = %d (%s)", bad.Code, bad.Body.String())
	}
	after, err := q.CountRecoveryCodes(ctx, user.ID)
	if err != nil {
		t.Fatalf("count after: %v", err)
	}
	if before != after {
		t.Fatalf("recovery codes rotated without a password: %d -> %d", before, after)
	}
	ok := accountDo(mux, http.MethodPost, "/account/totp/recovery", "password=Correct-horse-99%21", token)
	if ok.Code != http.StatusOK || !strings.Contains(ok.Body.String(), "Save these recovery codes now.") {
		t.Fatalf("correct-password recovery = %d", ok.Code)
	}
}

func TestAccountSessionShowsDevice(t *testing.T) {
	a, _, mux := accountFixture(t)
	ctx := auth.WithUserAgent(context.Background(),
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/120.0 Safari/537.36")
	_, token, err := a.Signup(ctx, "device@example.com", "Correct-horse-99!", "")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	page := accountDo(mux, http.MethodGet, "/account", "", token)
	if !strings.Contains(page.Body.String(), "Chrome on Linux") {
		t.Fatalf("account page does not name the session device:\n%s", page.Body.String())
	}
}

func TestDeviceLabel(t *testing.T) {
	cases := map[string]string{
		"": "Unknown device",
		"Mozilla/5.0 (Windows NT 10.0) Gecko/20100101 Firefox/121.0":                   "Firefox on Windows",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Version/17.0 Safari/605.1.15": "Safari on macOS",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) CriOS/120":             "Chrome on iOS",
	}
	for ua, want := range cases {
		if got := deviceLabel(ua); got != want {
			t.Fatalf("deviceLabel(%q) = %q, want %q", ua, got, want)
		}
	}
}

// TestDeviceTokenMintsForSession is the server-side issuance path: a signed-in
// user gets a token bound to their own id, carrying the cloud endpoint. No
// owner CLI is involved.
func TestDeviceTokenMintsForSession(t *testing.T) {
	a, q, _ := accountFixture(t)
	u, err := auth.EnsureOpenUser(context.Background(), q)
	if err != nil {
		t.Fatalf("open user: %v", err)
	}
	a.EnableOpen(u)
	t.Cleanup(a.DisableOpen)

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	cfg := &config.Config{
		LicenseMode:       config.LicenseAccount,
		LicensePrivateKey: hex.EncodeToString(priv),
		CloudSyncURL:      "nats://sync.example.test:4222",
		DeviceTokenDays:   30,
		DataDir:           t.TempDir(),
	}
	h := NewHandler(cfg, a, q)

	rec := httptest.NewRecorder()
	h.deviceToken(rec, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/account/device-token", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("device token: code=%d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "gogogo1.") {
		t.Fatal("page did not show a token")
	}
	idx := strings.Index(body, "gogogo1.")
	if idx < 0 {
		t.Fatal("token marker missing")
	}
	raw := body[idx:]
	if end := strings.Index(raw, "<"); end >= 0 {
		raw = raw[:end]
	}
	c, verr := license.Verify(pub, strings.TrimSpace(raw), time.Now())
	if verr != nil {
		t.Fatalf("minted token does not verify: %v", verr)
	}
	if c.Sub != u.ID || c.SyncURL != cfg.CloudSyncURL || c.ExpiresAt == 0 {
		t.Fatalf("claims = %+v", c)
	}
}
