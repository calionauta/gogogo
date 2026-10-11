// SCOPE:core — Auth on limen: signup/signin/session flow against the real
// Turso driver (red-proof for internal/auth).
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/db"
)

// testRouter boots a temp Turso database (migrations applied), builds the
// limen stack on it, and returns a chi router with the auth handler mounted
// plus a session-gated /me probe.
func testRouter(t *testing.T) *chi.Mux {
	t.Helper()

	sqlDB, err := db.OpenAt(t.TempDir() + "/auth-test.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("db migrate: %v", err)
	}

	a, aerr := New(sqlDB, []byte("0123456789abcdef0123456789abcdef"), false)
	if aerr != nil {
		t.Fatalf("auth new: %v", aerr)
	}

	r := chi.NewRouter()
	r.Mount("/", a.Handler())
	r.With(a.RequireSession).Get("/me", func(w http.ResponseWriter, req *http.Request) {
		sess, err := SessionFrom(req.Context())
		if err != nil {
			http.Error(w, "no session", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"email": sess.User.Email})
	})
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

func TestSignupIssuesGogogoAuthCookie(t *testing.T) {
	r := testRouter(t)

	rec := doSignup(t, r, "test@example.com", "Correct-horse-99!")
	if rec.Code != http.StatusOK {
		t.Fatalf("signup status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var found bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == CookieName && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no %q session cookie in signup response", CookieName)
	}
}

func TestSigninAndSessionGate(t *testing.T) {
	r := testRouter(t)

	signup := doSignup(t, r, "signin@example.com", "Correct-horse-99!")
	if signup.Code != http.StatusOK {
		t.Fatalf("signup status = %d (body: %s)", signup.Code, signup.Body.String())
	}

	// Wrong password must not authenticate.
	bad := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/auth/signin/credential",
		strings.NewReader(`{"credential":"signin@example.com","password":"wrong"}`))
	bad.Header.Set("Content-Type", "application/json")
	badRec := httptest.NewRecorder()
	r.ServeHTTP(badRec, bad)
	if badRec.Code == http.StatusOK {
		t.Fatal("signin with wrong password succeeded, want non-200")
	}

	// Right password signs in and issues the cookie.
	good := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/auth/signin/credential",
		strings.NewReader(`{"credential":"signin@example.com","password":"Correct-horse-99!"}`))
	good.Header.Set("Content-Type", "application/json")
	goodRec := httptest.NewRecorder()
	r.ServeHTTP(goodRec, good)
	if goodRec.Code != http.StatusOK {
		t.Fatalf("signin status = %d (body: %s)", goodRec.Code, goodRec.Body.String())
	}
	var sessionCookie *http.Cookie
	for _, c := range goodRec.Result().Cookies() {
		if c.Name == CookieName {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatalf("no %q cookie in signin response", CookieName)
	}

	// Session gate: no cookie -> 401.
	anon := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/me", nil)
	anonRec := httptest.NewRecorder()
	r.ServeHTTP(anonRec, anon)
	if anonRec.Code != http.StatusUnauthorized {
		t.Fatalf("anon /me status = %d, want 401", anonRec.Code)
	}

	// Session gate: cookie -> 200 with the user email.
	me := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/me", nil)
	me.AddCookie(sessionCookie)
	meRec := httptest.NewRecorder()
	r.ServeHTTP(meRec, me)
	if meRec.Code != http.StatusOK {
		t.Fatalf("/me status = %d (body: %s)", meRec.Code, meRec.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(meRec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode /me: %v", err)
	}
	if got["email"] != "signin@example.com" {
		t.Fatalf("/me email = %q, want signin@example.com", got["email"])
	}
}
