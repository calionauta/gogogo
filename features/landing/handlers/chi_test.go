// SCOPE:feature — HTTP landing/config contract tests.
package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

func handlersStack(t *testing.T) (http.Handler, *auth.Auth) {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/pages.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("db migrate: %v", merr)
	}
	a, aerr := auth.New(dbgen.New(sqlDB), false)
	if aerr != nil {
		t.Fatalf("auth new: %v", aerr)
	}
	cfg := config.Load()
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Routes())
	mux.Handle("/", NewHandler(cfg, a).Routes())
	return mux, a
}

func TestHandlersLandingPublic(t *testing.T) {
	mux, a := handlersStack(t)

	anon := httptest.NewRecorder()
	mux.ServeHTTP(anon, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	if anon.Code != http.StatusOK {
		t.Fatalf("anon / status = %d, want 200 (public page)", anon.Code)
	}

	// Signed-in users see the same hero with their email on it.
	ctx := context.Background()
	if _, _, err := a.Signup(ctx, "hero@example.com", "Correct-horse-99!", ""); err != nil {
		t.Fatalf("signup: %v", err)
	}
	cookie := loginCookie(t, mux, "hero@example.com")
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authed / status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "hero@example.com") {
		t.Fatal("authed hero missing the navbar email")
	}
}

// loginCookie signs in over HTTP and returns the session cookie.
func loginCookie(t *testing.T, mux http.Handler, email string) *http.Cookie {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/auth/signin/credential",
		strings.NewReader(`{"credential":"`+email+`","password":"Correct-horse-99!"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("signin status = %d", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return c
		}
	}
	t.Fatalf("no %q cookie", auth.CookieName)
	return nil
}
