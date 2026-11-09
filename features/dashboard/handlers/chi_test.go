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
	"github.com/calionauta/gogogo/features/dashboard/components"
	"github.com/calionauta/gogogo/features/todo"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/recipe"
	"github.com/calionauta/gogogo/internal/ui"

	"github.com/go-chi/chi/v5"
)

// stubProvider is a minimal recipe.Provider for the generic dashboard
// path: it only declares metrics (no routes served).
type stubProvider struct {
	id      string
	metrics []recipe.Metric
}

func (s *stubProvider) ID() string { return s.id }

func (s *stubProvider) Declaration() recipe.Declaration {
	return recipe.Declaration{Metrics: s.metrics}
}

func (s *stubProvider) Routes() chi.Router { return chi.NewRouter() }

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
	// Registry with the todo + notifications declarations (the generic
	// path): without it the dashboard legitimately renders no metric
	// cards (declared metrics live in the registry, not in the handler).
	reg := &recipe.Registry{}
	reg.Add(&stubProvider{id: "todo", metrics: []recipe.Metric{
		{Label: "Open", Query: "SELECT COUNT(*) FROM todos WHERE owner = ?", Signal: "openTodos"},
	}})
	reg.Add(&stubProvider{id: "notifications", metrics: []recipe.Metric{
		{Label: "Unread", Query: "SELECT COUNT(*) FROM notifications WHERE owner = ?", Signal: "inboxUnread"},
	}})
	f.h.SetRegistry(reg, f.sqlDB)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/dashboard", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /dashboard = %d, want 200", rec.Code)
	}
	// Metric cards (todo/inbox) + slot contributions only. Sidebar owns
	// navigation, so non-metric destinations must not appear as link cards
	// ("Open Whiteboard" CTA would prove a link card).
	body := rec.Body.String()
	for _, want := range []string{
		"Welcome back", "Todo", "Inbox", "Open", "Unread",
		"Pomodoro", "focus", `data-slot="dashboard.card"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q", want)
		}
	}
	if strings.Contains(body, "Open Whiteboard") {
		t.Fatalf("dashboard has a link card for Whiteboard; summary only")
	}
}

// TestBuildCardsListsInstalledRecipes pins the dashboard's "summary only"
// contract: one card per recipe with data, metrics from the Declaration
// (generic path) plus hook counts for chat. Sidebar owns navigation,
// config owns inventory — link-only destinations never appear.
func TestBuildCardsListsInstalledRecipes(t *testing.T) {
	mods := []ui.Module{
		{ID: ui.ModuleInbox, Title: "Inbox", URL: "/notifications"},
		{ID: "dashboard", Title: "Dashboard", URL: "/dashboard"},
		{ID: "todo", Title: "Todo", URL: "/todo"},
		{ID: "whiteboard", Title: "Whiteboard", URL: "/whiteboard"},
		{ID: "chat", Title: "Chat", URL: "/chat"},
		{ID: "graph", Title: "Graph", URL: "/graph"},
	}
	c := counts{
		declared: map[string][]components.Metric{
			"todo": {
				{Label: "Open", Value: 3, Signal: "openTodos"},
				{Label: "Done", Value: 4, Signal: "doneTodos"},
				{Label: "Total", Value: 7, Signal: "totalTodos"},
				{Label: "Shared", Value: 2, Signal: "sharedTodos"},
			},
			"notifications": {
				{Label: "Unread", Value: 1, Signal: "inboxUnread"},
				{Label: "Total", Value: 5, Signal: "totalInbox"},
			},
			"whiteboard": {{Label: "Boards", Value: 2, Signal: "boardsCount"}},
		},
		chatTotal: 10, chatHuman: 4,
	}
	cards := buildCards(mods, c)
	if len(cards) != 4 {
		t.Fatalf("buildCards = %d cards, want 4 (inbox, todo, whiteboard, chat)", len(cards))
	}
	if cards[0].Title != "Inbox" || len(cards[0].Metrics) != 2 ||
		cards[0].Metrics[0].Signal != "inboxUnread" || cards[0].Metrics[0].Value != 1 {
		t.Errorf("inbox card = %+v, want 2 metrics starting with inboxUnread=1", cards[0])
	}
	if cards[1].Title != "Todo" || len(cards[1].Metrics) != 4 ||
		cards[1].Metrics[0].Label != "Open" || cards[1].Metrics[0].Value != 3 {
		t.Errorf("todo card = %+v, want 4 metrics starting with Open=3", cards[1])
	}
	if cards[2].Title != "Whiteboard" || len(cards[2].Metrics) != 1 || cards[2].Metrics[0].Label != "Boards" {
		t.Errorf("whiteboard card = %+v, want one metric Boards", cards[2])
	}
	if cards[3].Title != "Chat" || len(cards[3].Metrics) != 2 || cards[3].Metrics[0].Value != 10 {
		t.Errorf("chat card = %+v, want hook metrics Messages=10 Human=4", cards[3])
	}
}

// TestBuildCardsEmptyPinsTheDegenerateBuild pins the only case the grid's
// empty state may fire: no recipes installed (a fully trimmed build). A
// build with bundled recipes always has cards, so the old message — "no
// extensions installed" while todo/inbox were on screen — cannot return.
func TestBuildCardsEmptyPinsTheDegenerateBuild(t *testing.T) {
	if cards := buildCards(nil, counts{}); len(cards) != 0 {
		t.Fatalf("buildCards(nil) = %v, want empty", cards)
	}
}
