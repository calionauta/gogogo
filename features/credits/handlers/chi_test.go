// SCOPE:plugin — HTTP credits contract tests (red-proof).
package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "turso.tech/database/tursogo"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/credits"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

func creditsStack(t *testing.T, enabled bool) http.Handler {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/app.db")
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
	var svc *credits.Service
	if enabled {
		ledger, err := OpenTursoLedger(t.TempDir())
		if err != nil {
			t.Fatalf("ledger: %v", err)
		}
		t.Cleanup(func() { ledger.Close() })
		cfg := testCreditsConfig()
		svc, err = credits.NewOnDB(ledger, cfg)
		if err != nil {
			t.Fatalf("credits engine: %v", err)
		}
	}
	h := NewHandler(a, svc)
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Handler())
	mux.Handle("/", h.Routes())
	return mux
}

func testCreditsConfig() *config.Config {
	cfg := config.Load()
	cfg.Credits.Enabled = true
	return cfg
}

func creditsSignup(t *testing.T, mux http.Handler, email string) *http.Cookie {
	t.Helper()
	body := `{"email":"` + email + `","password":"Correct-horse-99!"}`
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/auth/signup/credential", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("signup: status = %d", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func TestHandlersCreditsGates(t *testing.T) {
	mux := creditsStack(t, true)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/credits", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon balance status = %d, want 401 JSON (never a redirect)", rec.Code)
	}
}

func TestHandlersCreditsDisabled(t *testing.T) {
	mux := creditsStack(t, false)
	cookie := creditsSignup(t, mux, "off@example.com")
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/credits", nil)
	req.AddCookie(cookie)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled balance status = %d, want 503 (never a nil panic)", rec.Code)
	}
}

func TestHandlersCreditsBalance(t *testing.T) {
	mux := creditsStack(t, true)
	cookie := creditsSignup(t, mux, "bal@example.com")
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/credits", nil)
	req.AddCookie(cookie)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("balance status = %d (%s)", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `"balance"`) || !strings.Contains(body, `"mode"`) {
		t.Fatalf("balance body missing keys: %s", body)
	}
}
