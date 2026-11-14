// Dashboard contract tests: the page renders signed-in summary and recipe
// cards, and redirects guests to /login.
package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
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
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/recipe"
	"github.com/calionauta/gogogo/internal/ui"

	"github.com/go-chi/chi/v5"
)

// stubProvider is a minimal recipe.Provider for the generic dashboard
// path: it declares metrics and outcomes (no routes served).
type stubProvider struct {
	id       string
	metrics  []recipe.Metric
	outcomes []recipe.Outcome
}

func (s *stubProvider) ID() string { return s.id }

func (s *stubProvider) Declaration() recipe.Declaration {
	return recipe.Declaration{Metrics: s.metrics, Outcomes: s.outcomes}
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

// ownerOf resolves the test user's id through its session cookie, so the ratio
// fixture inserts rows the authenticated page actually counts.
func (f *fixture) ownerOf(t *testing.T, cookie *http.Cookie) string {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	sess, err := f.auth.Session(req)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	return sess.User.ID
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
		recipe.SlotDashboardCard: {Title: "Timer", Render: func() string { return "<p>focus</p>" }},
	}}); err != nil {
		t.Fatalf("add card: %v", err)
	}
	f.h.slots = slots
	// Registry with the todo + notifications declarations (the generic
	// path): without it the dashboard legitimately renders no metric
	// cards (declared metrics live in the registry, not in the handler).
	reg := &recipe.Registry{}
	reg.Add(&stubProvider{id: "todo", metrics: []recipe.Metric{
		{Label: "Open", Query: "SELECT COUNT(*) FROM ext_todo_todos WHERE owner = ?", Signal: "openTodos"},
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
		"Timer", "focus", `data-slot="dashboard.card"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q", want)
		}
	}
	if strings.Contains(body, "Open Whiteboard") {
		t.Fatalf("dashboard has a link card for Whiteboard; summary only")
	}
}

// TestDashboardRendersDeclaredOutcomeRatio closes the loop the declaration
// opens: the receipt's pair runs against the recipe's own rows through the same
// generic COUNT executor as a metric, and the card renders the ratio. Revert
// declaredOutcomes and the card loses its "1/2 · 50%"; drop the owner binding
// and the ratio goes to 2/3 because the other owner's row leaks in.
func TestDashboardRendersDeclaredOutcomeRatio(t *testing.T) {
	f := newFixture(t)
	cookie := f.signup(t, "outcome@example.com")
	if _, err := f.sqlDB.ExecContext(context.Background(),
		`CREATE TABLE ext_probe_orders (owner TEXT NOT NULL, corrected INTEGER NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	owner := f.ownerOf(t, cookie)
	rows := []struct {
		owner     string
		corrected int
	}{
		{owner, 0}, {owner, 1}, {"someone-else", 0},
	}
	for _, r := range rows {
		if _, err := f.sqlDB.ExecContext(context.Background(),
			`INSERT INTO ext_probe_orders (owner, corrected) VALUES (?, ?)`, r.owner, r.corrected); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// The card set is keyed off the rail destination, so the declared outcome
	// rides the todo card here; the queries read this test's own table, which is
	// the whole point of the generic path.
	reg := &recipe.Registry{}
	reg.Add(&stubProvider{
		id:      "todo",
		metrics: []recipe.Metric{{Label: "Orders", Query: "SELECT COUNT(*) FROM ext_probe_orders WHERE owner = ?"}},
		outcomes: []recipe.Outcome{{
			ID: "probe_without_repair", Job: "Order without fixing it", Beneficiary: "user",
			Success: recipe.Metric{
				Label: "Without repair",
				Query: "SELECT COUNT(*) FROM ext_probe_orders WHERE owner = ? AND corrected = 0",
			},
			Total: recipe.Metric{
				Label: "Orders",
				Query: "SELECT COUNT(*) FROM ext_probe_orders WHERE owner = ?",
			},
			CounterSignals: []string{"corrected"},
		}},
	})
	f.h.SetRegistry(reg, f.sqlDB)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/dashboard", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /dashboard = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Without repair", "Orders", "1/2 · 50%"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard body missing %q", want)
		}
	}
}

// TestBuildCardsListsInstalledRecipes pins the dashboard's "summary only"
// contract: one card per rail destination with data, metrics from the
// Declaration (generic path) plus hook counts for chat. Sidebar owns
// navigation, config owns inventory — link-only destinations never appear.
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
			"whiteboard": {{Label: "Total", Value: 2, Signal: "boardsTotal"}},
		},
		chatTotal: 10, chatHuman: 4,
	}
	cards := buildCards(mods, reachAll(mods), c)
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
	if cards[2].Title != "Whiteboard" || len(cards[2].Metrics) != 1 || cards[2].Metrics[0].Label != "Total" {
		t.Errorf("whiteboard card = %+v, want one metric Total", cards[2])
	}
	if cards[3].Title != "Chat" || len(cards[3].Metrics) != 2 || cards[3].Metrics[0].Value != 10 {
		t.Errorf("chat card = %+v, want hook metrics Messages=10 Human=4", cards[3])
	}
}

// TestBuildCardsKeepsUnreachableRecipesWithoutALink pins the two halves of
// the grid's role contract: every destination that reports data gets a card
// (the grid summarizes the instance), but a destination the role cannot
// open gets NO button — a link that 403s is a broken promise.
func TestBuildCardsKeepsUnreachableRecipesWithoutALink(t *testing.T) {
	mods := []ui.Module{
		{ID: ui.ModuleInbox, Title: "Inbox", URL: "/notifications"},
		{ID: "todo", Title: "Todo", URL: "/todo"},
	}
	c := counts{
		declared: map[string][]components.Metric{
			"todo":          {{Label: "Open", Value: 3, Signal: "openTodos"}},
			"notifications": {{Label: "Unread", Value: 1, Signal: "inboxUnread"}},
		},
	}
	// The role may open Todo but not the Inbox.
	cards := buildCards(mods, map[string]bool{"todo": true}, c)
	if len(cards) != 2 {
		t.Fatalf("buildCards = %d cards, want 2 (the role does not hide a recipe)", len(cards))
	}
	if cards[0].Title != "Inbox" {
		t.Fatalf("card 0 = %q, want the Inbox (every destination keeps its card)", cards[0].Title)
	}
	if cards[0].Cta != "" || !cards[0].Locked {
		t.Errorf("inbox card = %+v, want no CTA and Locked", cards[0])
	}
	if cards[1].Cta == "" || cards[1].Locked {
		t.Errorf("todo card = %+v, want a CTA and no Locked", cards[1])
	}
}

// TestBuildCardsEmptyPinsTheDegenerateBuild pins the only case the grid's
// empty state may fire: no recipes installed (a fully trimmed build). A
// build with bundled recipes always has cards, so the old message — "no
// extensions installed" while todo/inbox were on screen — cannot return.
func TestBuildCardsEmptyPinsTheDegenerateBuild(t *testing.T) {
	if cards := buildCards(nil, nil, counts{}); len(cards) != 0 {
		t.Fatalf("buildCards(nil) = %v, want empty", cards)
	}
}

// reachAll is the "every destination is open" set, for the tests that care
// about the cards themselves and not about the role's reach.
func reachAll(mods []ui.Module) map[string]bool {
	out := make(map[string]bool, len(mods))
	for _, m := range mods {
		out[m.ID] = true
	}
	return out
}

// TestDeclaredMetricsLogAFailingQuery pins the silence fix. A metric whose
// query fails is omitted from the grid — and LOGGED: the silent skip is
// how a dashboard loses a card (a recipe pointing at a table it no longer
// owns) and nobody notices.
func TestDeclaredMetricsLogAFailingQuery(t *testing.T) {
	f := newFixture(t)
	buf := captureDashboardLogs(t)
	reg := &recipe.Registry{}
	reg.Add(&stubProvider{id: "broken", metrics: []recipe.Metric{
		{Label: "Fake", Query: "SELECT COUNT(*) FROM missing_table WHERE owner = ?"},
	}})
	h := NewHandler(f.h.cfg, f.auth, f.q, nil)
	h.SetRegistry(reg, f.sqlDB)
	if got := h.declaredMetrics(context.Background(), "owner-1"); len(got) != 0 {
		t.Fatalf("declaredMetrics = %v, want no metric for a failing query", got)
	}
	out := buf.String()
	for _, want := range []string{
		`"level":"WARN"`,
		`"msg":"dashboard: metric query failed"`,
		`"recipe":"broken"`,
		`"metric":"Fake"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q:\n%s", want, out)
		}
	}
}

// captureDashboardLogs redirects the default slog logger into a buffer for
// the duration of one test. Restored on cleanup.
func captureDashboardLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	observability.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { observability.SetDefault(prev) })
	return &buf
}

// that declares a COUNT query yields its metric with the value the
// query returns, and a provider with a broken query is omitted (not a
// fake zero). This is the user-recipe contract — a recipe that today
// stores no DB rows simply declares nothing.
func TestDeclaredMetricsExecutesCounts(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	owner := "owner-1"
	// One real row in the todo unit's OWN table so the COUNT has something
	// to say (the core `todos` seed is dropped once the unit's migration
	// set lands). The user must exist first (FK owner → users.id).
	if _, err := f.sqlDB.ExecContext(ctx,
		"INSERT INTO users (id, email, password_hash, created, updated) VALUES (?, ?, ?, ?, ?)",
		owner, "owner@example.com", "x", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	const insertTodo = "INSERT INTO ext_todo_todos (id, owner, title, completed, visibility, created, updated) " +
		"VALUES (?, ?, ?, 0, 'private', ?, ?)"
	if _, err := f.sqlDB.ExecContext(ctx, insertTodo,
		"id1", owner, "hello", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatalf("insert todo: %v", err)
	}
	reg := &recipe.Registry{}
	reg.Add(&stubProvider{id: "todo", metrics: []recipe.Metric{
		{Label: "Open", Query: "SELECT COUNT(*) FROM ext_todo_todos WHERE owner = ? AND completed = 0", Signal: "openTodos"},
	}})
	reg.Add(&stubProvider{id: "broken", metrics: []recipe.Metric{
		{Label: "Fake", Query: "SELECT nope FROM missing_table WHERE owner = ?"},
	}})
	h := NewHandler(f.h.cfg, f.auth, f.q, nil)
	h.SetRegistry(reg, f.sqlDB)
	got := h.declaredMetrics(ctx, owner)
	m := got["todo"]
	if len(m) != 1 || m[0].Value != 1 || m[0].Label != "Open" || m[0].Signal != "openTodos" {
		t.Fatalf("todo declared = %+v, want one metric Open=1", m)
	}
	if _, ok := got["broken"]; ok {
		t.Fatalf("broken provider must be omitted, got %v", got["broken"])
	}
}
