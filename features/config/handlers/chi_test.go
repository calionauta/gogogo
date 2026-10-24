// SCOPE:feature — HTTP config view contract tests.
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
	"github.com/calionauta/gogogo/internal/rbac"

	// The page renders inside internal/ui.Shell, which injects the active
	// skin's assets — production registers the skin from cmd/web's blank
	// imports, so the test must too (skins.Active panics on an empty
	// registry, by design: a page rendering without a skin is a boot bug).
	_ "github.com/calionauta/gogogo/web/skins/daisyui"
)

func TestHandlersConfigGate(t *testing.T) {
	sqlDB, err := db.OpenAt(t.TempDir() + "/config.db")
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
	mux.Handle("/", NewHandler(cfg, a, dbgen.New(sqlDB)).Routes())

	anon := httptest.NewRecorder()
	mux.ServeHTTP(anon, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/config", nil))
	if anon.Code != http.StatusForbidden {
		t.Fatalf("anon /config status = %d, want 403", anon.Code)
	}

	ctx := context.Background()
	if _, _, err := a.Signup(ctx, "op@example.com", "Correct-horse-99!", ""); err != nil {
		t.Fatalf("signup: %v", err)
	}
	promoteConfigAdmin(t, dbgen.New(sqlDB), "op@example.com")
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

// TestChiConfigRoleBanner pins the Casbin demo surface: the page shows
// the viewer's role and the enforcement posture. A fresh signup is
// 'user' (migration 0008 default); RBAC is off in tests (pass-through).
// Red-proof: drop the banner from page.templ and both assertions go red.
func TestChiConfigRoleBanner(t *testing.T) {
	sqlDB, err := db.OpenAt(t.TempDir() + "/config-role.db")
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
	mux.Handle("/", NewHandler(cfg, a, dbgen.New(sqlDB)).Routes())

	ctx := context.Background()
	if _, _, serr := a.Signup(ctx, "role@example.com", "Correct-horse-99!", ""); serr != nil {
		t.Fatalf("signup: %v", serr)
	}
	promoteConfigAdmin(t, dbgen.New(sqlDB), "role@example.com")
	token, _, lerr := a.Login(ctx, "role@example.com", "Correct-horse-99!")
	if lerr != nil {
		t.Fatalf("login: %v", err)
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
	if !strings.Contains(body, rbac.RoleAdmin) {
		t.Fatalf("config view missing the viewer role:\n%s", body)
	}
	if !strings.Contains(body, "RBAC_ENABLED=true") {
		t.Fatalf("config view missing the enforcement posture hint")
	}
}

func promoteConfigAdmin(t *testing.T, q *dbgen.Queries, email string) {
	t.Helper()
	if err := q.SetUserRole(context.Background(), dbgen.SetUserRoleParams{
		Role: rbac.RoleAdmin, Updated: dbschema.Now(), Email: email,
	}); err != nil {
		t.Fatalf("promote %s: %v", email, err)
	}
}
