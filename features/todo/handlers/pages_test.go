// SCOPE:feature — HTTP page contract tests (login + index, step 6).
//
// Same red-proof fixture as the data endpoints: real Turso, real auth,
// driven over HTTP. Contracts: paths, redirects, rendered components.
package handlers

import (
	"net/http"
	"strings"
	"testing"

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
		t.Fatal("index missing the navbar user email")
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
