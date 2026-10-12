// SCOPE:core — Flat auth tests (owned argon2id + sessions).
//
// Every test runs against a real migrated Turso file: password hashing,
// session issue/validate/revoke, cookie handling and the HTTP surface.
package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/auth-flat.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := dbschema.Migrate(sqlDB); err != nil {
		t.Fatalf("db migrate: %v", err)
	}
	return sqlDB
}

func testAuth(t *testing.T, sqlDB *sql.DB) *Auth {
	t.Helper()
	a, err := New(dbgen.New(sqlDB), false)
	if err != nil {
		t.Fatalf("auth new: %v", err)
	}
	return a
}

func TestPasswordHashRoundTrip(t *testing.T) {
	withRealArgon(t)
	hash, err := hashPassword("Correct-horse-99!")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if ok, stale := verifyPassword(hash, "Correct-horse-99!"); !ok || stale {
		t.Fatalf("valid password rejected (ok=%v stale=%v)", ok, stale)
	}
	if ok, _ := verifyPassword(hash, "wrong"); ok {
		t.Fatal("wrong password accepted")
	}
	if ok, _ := verifyPassword("garbage", "Correct-horse-99!"); ok {
		t.Fatal("malformed hash accepted")
	}
	if ok, _ := verifyPassword("argon2id$v=9$dGVzdA$ZHVtbXk", "Correct-horse-99!"); ok {
		t.Fatal("future version accepted")
	}
	if _, err := hashPassword("short"); err == nil {
		t.Fatal("short password accepted")
	}
}

// TestPasswordRotation stages a parameter rotation without waiting for a
// real one: v=1 rows verify under their own entry, report stale, and Login
// upgrades them to the staged current version (rehash-on-login, the whole
// point of the version marker). Delete the upgrade and the row stays v=1.
func TestPasswordRotation(t *testing.T) {
	ctx := context.Background()
	a := testAuth(t, testDB(t))
	const pw = "Correct-horse-99!"
	old, herr := hashPassword(pw)
	if herr != nil {
		t.Fatalf("hash: %v", herr)
	}
	if !strings.HasPrefix(old, "argon2id$v=1$") {
		t.Fatalf("hash = %q, want v=1", old)
	}
	// Stage v=2 (cheaper params keep the test fast) and point current at it.
	oldVersions, oldCurrent := argonVersions, argonVersion
	argonVersions = map[uint32]argonParams{
		1: oldVersions[1],
		2: {time: 1, memory: 8 * 1024, threads: 1, keyLen: 32},
	}
	argonVersion = 2
	defer func() { argonVersions, argonVersion = oldVersions, oldCurrent }()
	if _, _, err := a.Signup(ctx, "rot@example.com", pw, ""); err != nil {
		t.Fatalf("signup: %v", err)
	}
	// Backdate the row to v=1 (as if created before the rotation).
	row, rerr := a.q.GetUserByEmail(ctx, "rot@example.com")
	if rerr != nil {
		t.Fatalf("lookup: %v", rerr)
	}
	if err := a.q.UpdateUserPassword(ctx, dbgen.UpdateUserPasswordParams{
		PasswordHash: old, Updated: dbschema.Now(), ID: row.ID,
	}); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	if _, _, lerr := a.Login(ctx, "rot@example.com", pw); lerr != nil {
		t.Fatalf("login on stale hash: %v", lerr)
	}
	row, rerr = a.q.GetUserByEmail(ctx, "rot@example.com")
	if rerr != nil {
		t.Fatalf("relookup: %v", rerr)
	}
	if !strings.HasPrefix(row.PasswordHash, "argon2id$v=2$") {
		t.Fatalf("row still %q, want upgraded v=2", row.PasswordHash)
	}
	if ok, stale := verifyPassword(row.PasswordHash, pw); !ok || stale {
		t.Fatalf("upgraded hash rejected (ok=%v stale=%v)", ok, stale)
	}
}

func TestEmailNormalization(t *testing.T) {
	addr, err := normalizeEmail("  Test@Example.COM ")
	if err != nil || addr != "test@example.com" {
		t.Fatalf("normalize = %q, %v; want test@example.com", addr, err)
	}
	if _, err := normalizeEmail("not-an-email"); err == nil {
		t.Fatal("invalid email accepted")
	}
}

func TestSignupLoginLogout(t *testing.T) {
	ctx := context.Background()
	a := testAuth(t, testDB(t))

	user, token, err := a.Signup(ctx, "Flat@Example.com", "Correct-horse-99!", "Flat")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	if user.Email != "flat@example.com" {
		t.Fatalf("signup email = %q, want lowercased", user.Email)
	}
	if token == "" {
		t.Fatal("signup issued no token")
	}

	// Duplicate email: same sentinel as a wrong password.
	if _, _, derr := a.Signup(ctx, "flat@example.com", "Correct-horse-99!", ""); !errors.Is(derr, ErrBadCredentials) {
		t.Fatalf("duplicate signup err = %v, want ErrBadCredentials", derr)
	}

	// Unknown email and wrong password are indistinguishable.
	if _, _, uerr := a.Login(ctx, "nobody@example.com", "Correct-horse-99!"); !errors.Is(uerr, ErrBadCredentials) {
		t.Fatalf("unknown email err = %v, want ErrBadCredentials", uerr)
	}
	if _, _, werr := a.Login(ctx, "flat@example.com", "wrong-password-00"); !errors.Is(werr, ErrBadCredentials) {
		t.Fatalf("wrong password err = %v, want ErrBadCredentials", werr)
	}

	token2, totp, err := a.Login(ctx, "FLAT@example.com", "Correct-horse-99!")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if totp {
		t.Fatal("non-2FA login returned totpRequired")
	}

	// Both tokens validate.
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	// #nosec G124 — test-only request cookie; never traverses a network.
	req.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	sess, err := a.Session(req)
	if err != nil {
		t.Fatalf("session(token1): %v", err)
	}
	if sess.User.ID != user.ID {
		t.Fatalf("session user = %q, want %q", sess.User.ID, user.ID)
	}

	// Logout revokes; repeating is safe.
	a.Logout(ctx, token2)
	if _, err := a.Session(cookieReq(token2)); err == nil {
		t.Fatal("logged-out token still valid")
	}
	a.Logout(ctx, token2)
	a.Logout(ctx, "")
}

func cookieReq(token string) *http.Request {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	// #nosec G124 — test-only request cookie; never traverses a network.
	req.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	return req
}

// testRouter boots the HTTP surface: auth handler plus a session-gated
// /me probe behind RequireSession.
func testRouter(t *testing.T) http.Handler {
	t.Helper()
	a := testAuth(t, testDB(t))

	r := http.NewServeMux()
	r.Handle("/api/auth/", a.Handler())
	r.Handle("GET /me", a.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		sess, err := SessionFrom(req.Context())
		if err != nil {
			http.Error(w, "no session", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"email": sess.User.Email})
	})))
	return r
}

func doSignup(t *testing.T, r http.Handler, email, password string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"email":` + strconv.Quote(email) + `,"password":` + strconv.Quote(password) + `}`
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/auth/signup/credential", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestHTTPFlow(t *testing.T) {
	r := testRouter(t)

	rec := doSignup(t, r, "http@example.com", "Correct-horse-99!")
	if rec.Code != http.StatusOK {
		t.Fatalf("signup status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == CookieName && c.Value != "" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatalf("no %q cookie in signup response", CookieName)
	}
	if !cookie.HttpOnly {
		t.Error("session cookie missing HttpOnly")
	}

	// Duplicate signup → 401, same as a bad login (no enumeration).
	dup := doSignup(t, r, "http@example.com", "Correct-horse-99!")
	if dup.Code != http.StatusUnauthorized {
		t.Fatalf("duplicate signup status = %d, want 401", dup.Code)
	}

	// Signin round-trips through the same cookie.
	signinReq := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/auth/signin/credential",
		strings.NewReader(`{"credential":"http@example.com","password":"Correct-horse-99!"}`))
	signinReq.Header.Set("Content-Type", "application/json")
	signinRec := httptest.NewRecorder()
	r.ServeHTTP(signinRec, signinReq)
	if signinRec.Code != http.StatusOK {
		t.Fatalf("signin status = %d (%s)", signinRec.Code, signinRec.Body.String())
	}

	badReq := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/auth/signin/credential",
		strings.NewReader(`{"credential":"http@example.com","password":"wrong-password-00"}`))
	badReq.Header.Set("Content-Type", "application/json")
	badRec := httptest.NewRecorder()
	r.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusUnauthorized {
		t.Fatalf("bad signin status = %d, want 401", badRec.Code)
	}

	// Gate: cookie → email, anon → 401.
	me := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/me", nil)
	me.AddCookie(cookie)
	meRec := httptest.NewRecorder()
	r.ServeHTTP(meRec, me)
	if meRec.Code != http.StatusOK {
		t.Fatalf("/me status = %d", meRec.Code)
	}
	var got map[string]string
	if err := json.Unmarshal(meRec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode /me: %v", err)
	}
	if got["email"] != "http@example.com" {
		t.Fatalf("/me email = %q", got["email"])
	}
	anonRec := httptest.NewRecorder()
	r.ServeHTTP(anonRec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/me", nil))
	if anonRec.Code != http.StatusUnauthorized {
		t.Fatalf("anon /me status = %d, want 401", anonRec.Code)
	}

	// Signout revokes.
	outReq := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/auth/signout", nil)
	outReq.AddCookie(cookie)
	outRec := httptest.NewRecorder()
	r.ServeHTTP(outRec, outReq)
	if outRec.Code != http.StatusOK {
		t.Fatalf("signout status = %d", outRec.Code)
	}
	me2 := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/me", nil)
	me2.AddCookie(cookie)
	me2Rec := httptest.NewRecorder()
	r.ServeHTTP(me2Rec, me2)
	if me2Rec.Code != http.StatusUnauthorized {
		t.Fatalf("/me after signout status = %d, want 401", me2Rec.Code)
	}
}

func TestSessionExpiry(t *testing.T) {
	ctx := context.Background()
	sqlDB := testDB(t)
	a := testAuth(t, sqlDB)

	user, token, err := a.Signup(ctx, "exp@example.com", "Correct-horse-99!", "")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	_ = user

	// Backdate the session past its TTL straight in the table.
	past := time.Now().UTC().Add(-time.Hour).Format(dbschema.TimeFormat)
	sum := tokenHash(token)
	if _, err := sqlDB.ExecContext(ctx,
		`UPDATE sessions SET expires = ? WHERE token_hash = ?`, past, sum); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	if _, err := a.Session(cookieReq(token)); err == nil {
		t.Fatal("expired session accepted")
	}
	// Expired sessions are reaped on read.
	var n int
	if err := sqlDB.QueryRowContext(ctx,
		`SELECT count(*) FROM sessions WHERE token_hash = ?`, sum).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatal("expired session row survived validation")
	}
}
