// SCOPE:feature — Chi config view contract tests.
package chi

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
)

func TestChiConfigGate(t *testing.T) {
	sqlDB, err := db.OpenAt(t.TempDir() + "/config.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if merr := db.Migrate(sqlDB); merr != nil {
		t.Fatalf("db migrate: %v", merr)
	}
	a, aerr := auth.New(dbgen.New(sqlDB), false)
	if aerr != nil {
		t.Fatalf("auth new: %v", aerr)
	}
	cfg := config.Load()
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Handler())
	mux.Handle("/", NewHandler(cfg, a).Routes())

	anon := httptest.NewRecorder()
	mux.ServeHTTP(anon, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/config", nil))
	if anon.Code != http.StatusSeeOther {
		t.Fatalf("anon /config status = %d, want 303", anon.Code)
	}

	ctx := context.Background()
	if _, _, err := a.Signup(ctx, "op@example.com", "Correct-horse-99!", ""); err != nil {
		t.Fatalf("signup: %v", err)
	}
	token, _, lerr := a.Login(ctx, "op@example.com", "Correct-horse-99!")
	if lerr != nil {
		t.Fatalf("login: %v", lerr)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/config", nil)
	// #nosec G124 — test-only request cookie.
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authed /config status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "op@example.com") {
		t.Fatal("config view missing the user email")
	}
}
