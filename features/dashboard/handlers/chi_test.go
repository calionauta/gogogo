// Dashboard contract tests: the page renders signed-in summary and recipe
// cards, and redirects guests to /login.
package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/todo"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/recipe"
)

type fixture struct {
	r     http.Handler
	h     *Handler
	q     *dbgen.Queries
	sqlDB *sql.DB
	auth  *auth.Auth
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/dashboard.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("migrate: %v", merr)
	}
	set := dbschema.MigrationSet{ID: "todo", FS: todo.Migrations, Dir: "migrations"}
	if merr := dbschema.MigrateWith(sqlDB, set); merr != nil {
		t.Fatalf("todo migrate: %v", merr)
	}
	a, err := auth.New(dbgen.New(sqlDB), false)
	if err != nil {
		t.Fatalf("auth new: %v", err)
	}
	cfg := config.Load()
	h := NewHandler(cfg, a, dbgen.New(sqlDB), nil)
	mux := http.NewServeMux()
	mux.Handle("/", h.Routes())
	return &fixture{r: mux, h: h, q: dbgen.New(sqlDB), sqlDB: sqlDB, auth: a}
}

func (f *fixture) signup(t *testing.T, email string) *http.Cookie {
	t.Helper()
	body := `{"email":` + `"` + email + `"` + `,"password":"long-password-123456789"}`
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/auth/signup/credential", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	// Auth routes are mounted by the server; here drive them via the handler's own
	// auth object through a mux that knows the base path.
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", f.auth.Routes())
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("signup %s: %d %s", email, rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName {
			return c
		}
	}
	t.Fatalf("no session cookie for %s", email)
	return nil
}

func TestDashboardRequiresSession(t *testing.T) {
	f := newFixture(t)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/dashboard", nil)
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("guest GET /dashboard = %d, want 303", rec.Code)
	}
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if loc.Path != "/login" {
		t.Fatalf("guest redirect = %q, want /login", loc.Path)
	}
}

func TestDashboardRendersSummaryAndCards(t *testing.T) {
	f := newFixture(t)
	cookie := f.signup(t, "dash@example.com")
	slots := recipe.NewSlotRegistry()
	if err := slots.Add(recipe.Contribution{Fragments: map[recipe.SlotID]recipe.Fragment{
		recipe.SlotDashboardCard: {Title: "Pomodoro", Render: func() string { return "<p>focus</p>" }},
	}}); err != nil {
		t.Fatalf("add card: %v", err)
	}
	f.h.slots = slots
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/dashboard", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /dashboard = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Welcome back", "Pomodoro", "focus", `data-slot="dashboard.card"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q", want)
		}
	}
}

func TestDashboardEmptyStateWithoutCards(t *testing.T) {
	f := newFixture(t)
	cookie := f.signup(t, "empty@example.com")
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/dashboard", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /dashboard = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No extensions installed yet") {
		t.Fatalf("empty grid must render its empty state")
	}
}
