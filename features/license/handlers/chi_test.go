package handlers

import (
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/cloudcfg"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/license"
)

func testStack(t *testing.T, mode string) (*Handler, *license.Store, ed25519.PrivateKey, *dbgen.Queries) {
	t.Helper()
	sqlDB, err := db.OpenAt(filepath.Join(t.TempDir(), "activate.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("migrate: %v", merr)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	store, err := license.Open(filepath.Join(t.TempDir(), "license.token"), pub)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	a, err := auth.New(dbgen.New(sqlDB), false)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	cfg := &config.Config{LicenseMode: mode, DataDir: t.TempDir()}
	q := dbgen.New(sqlDB)
	return NewHandler(cfg, store, a, q), store, priv, q
}

func postToken(t *testing.T, h *Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"token": {token}}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/activate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.activate(rec, req)
	return rec
}

// TestActivateLicenseMode pins the offline path: a valid token activates and
// redirects, an invalid one renders the page again with an error.
func TestActivateLicenseMode(t *testing.T) {
	h, store, priv, _ := testStack(t, config.LicenseRequired)

	rec := postToken(t, h, "gogogo1.garbage.garbage")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "alert-error") {
		t.Fatalf("invalid token: code=%d body=%q", rec.Code, rec.Body.String())
	}
	if store.Valid(time.Now()) {
		t.Fatal("invalid token activated the store")
	}

	token, err := license.Sign(priv, license.Claims{Plan: "pro", SyncURL: "nats://sync.example.test:4222"})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	rec = postToken(t, h, token)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("valid token: code=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	if !store.Valid(time.Now()) {
		t.Fatal("valid token did not activate the store")
	}
	if c, cerr := cloudcfg.Load(cloudcfg.Path(h.cfg.DataDir)); cerr != nil || c == nil {
		t.Fatalf("token sync endpoint not applied: %+v, %v", c, cerr)
	}
}

// TestActivateAccountModeAdoptsIdentity is the identity alignment: the local
// user is created with the token's sub, and the response carries a session
// cookie so the app is signed in without a password.
func TestActivateAccountModeAdoptsIdentity(t *testing.T) {
	h, _, priv, q := testStack(t, config.LicenseAccount)
	const sub = "11111111-1111-7111-8111-111111111111"
	token, err := license.Sign(priv, license.Claims{Sub: sub, Email: "buyer@example.test"})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	form := url.Values{"token": {token}, "password": {"correct-horse-9"}}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/activate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.activate(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("activate: code=%d body=%q", rec.Code, rec.Body.String())
	}
	row, err := q.GetUser(t.Context(), sub)
	if err != nil {
		t.Fatalf("adopted user missing: %v", err)
	}
	if row.Email != "buyer@example.test" {
		t.Fatalf("email = %q", row.Email)
	}
	if row.PasswordHash == "!" {
		t.Fatal("local password was not stored")
	}
	if !strings.Contains(rec.Header().Get("Set-Cookie"), auth.CookieName+"=") {
		t.Fatalf("no session cookie in %v", rec.Header())
	}
	login, _, lerr := h.a.Login(t.Context(), "buyer@example.test", "correct-horse-9")
	if lerr != nil || login == "" {
		t.Fatalf("local password not set: %v", lerr)
	}
}

// TestActivateAccountModeRequiresSub refuses an unbound token instead of
// creating a local account that can never converge with the server.
func TestActivateAccountModeRequiresSub(t *testing.T) {
	h, store, priv, _ := testStack(t, config.LicenseAccount)
	token, _ := license.Sign(priv, license.Claims{Email: "x@y.test"})
	form := url.Values{"token": {token}, "password": {"correct-horse-9"}}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/activate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.activate(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "not bound to an account") {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	if !store.Valid(time.Now()) {
		t.Fatal("the token verified but the store was left inactive")
	}
}
