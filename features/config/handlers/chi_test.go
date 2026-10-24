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
	csvAnon := httptest.NewRecorder()
	mux.ServeHTTP(csvAnon, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/config/audit.csv", nil))
	if csvAnon.Code != http.StatusForbidden {
		t.Fatalf("anon /config/audit.csv status = %d, want 403", csvAnon.Code)
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
	if !strings.Contains(body, "RBAC enforcement is <strong>on</strong>") {
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

func TestChiConfigUserAdminActions(t *testing.T) {
	sqlDB, err := db.OpenAt(t.TempDir() + "/config-users.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("db migrate: %v", merr)
	}
	q := dbgen.New(sqlDB)
	a, aerr := auth.New(q, false)
	if aerr != nil {
		t.Fatalf("auth: %v", aerr)
	}
	cfg := config.Load()
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Routes())
	mux.Handle("/", NewHandler(cfg, a, q).Routes())
	ctx := context.Background()
	if _, _, serr := a.Signup(ctx, "admin2@example.com", "Correct-horse-99!", ""); serr != nil {
		t.Fatalf("admin signup: %v", serr)
	}
	if _, _, serr := a.Signup(ctx, "target@example.com", "Correct-horse-99!", ""); serr != nil {
		t.Fatalf("target signup: %v", serr)
	}
	promoteConfigAdmin(t, q, "admin2@example.com")
	adminToken, _, lerr := a.Login(ctx, "admin2@example.com", "Correct-horse-99!")
	if lerr != nil {
		t.Fatalf("admin login: %v", lerr)
	}
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/config/users/role",
		strings.NewReader("email=target%40example.com&role=viewer"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// #nosec G124 — test-only request cookie.
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: adminToken})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("role change = %d (%s)", rec.Code, rec.Body.String())
	}
	row, err := q.GetUserByEmail(ctx, "target@example.com")
	if err != nil {
		t.Fatalf("target: %v", err)
	}
	if row.Role != "viewer" {
		t.Fatalf("role = %q, want viewer", row.Role)
	}
}

// TestAdminRoleChangeIsAudited pins the operator trail: a successful role
// change must leave a row the security panel can render, naming the target
// and the actor. Red-proof: drop the RecordAudit call and the event is gone.
func TestAdminRoleChangeIsAudited(t *testing.T) {
	sqlDB, err := db.OpenAt(t.TempDir() + "/config-audit.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("db migrate: %v", merr)
	}
	q := dbgen.New(sqlDB)
	a, aerr := auth.New(q, false)
	if aerr != nil {
		t.Fatalf("auth new: %v", aerr)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Routes())
	mux.Handle("/", NewHandler(config.Load(), a, q).Routes())

	ctx := context.Background()
	if _, _, serr := a.Signup(ctx, "actor@example.com", "Correct-horse-99!", ""); serr != nil {
		t.Fatalf("signup actor: %v", serr)
	}
	if _, _, serr := a.Signup(ctx, "target@example.com", "Correct-horse-99!", ""); serr != nil {
		t.Fatalf("signup target: %v", serr)
	}
	promoteConfigAdmin(t, q, "actor@example.com")
	actor, _, lerr := a.Login(ctx, "actor@example.com", "Correct-horse-99!")
	if lerr != nil {
		t.Fatalf("login actor: %v", lerr)
	}
	actorSess, serr := a.SessionByToken(ctx, actor)
	if serr != nil {
		t.Fatalf("resolve actor: %v", serr)
	}

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/config/users/role",
		strings.NewReader("email=target%40example.com&role=viewer"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// #nosec G124 — test-only request cookie.
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: actor})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("role change status = %d, want 303", rec.Code)
	}

	csvReq := httptest.NewRequestWithContext(ctx, http.MethodGet, "/config/audit.csv", nil)
	// #nosec G124 — test-only request cookie.
	csvReq.AddCookie(&http.Cookie{Name: auth.CookieName, Value: actor})
	csvRec := httptest.NewRecorder()
	mux.ServeHTTP(csvRec, csvReq)
	if csvRec.Code != http.StatusOK {
		t.Fatalf("audit csv status = %d", csvRec.Code)
	}
	if ct := csvRec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("audit csv content-type = %q", ct)
	}
	if !strings.Contains(csvRec.Body.String(), "admin role changed") ||
		!strings.Contains(csvRec.Body.String(), "target@example.com") {
		t.Fatalf("audit csv missing the role change:\n%s", csvRec.Body.String())
	}

	rows, qerr := q.ListSecurityEvents(ctx, 50)
	if qerr != nil {
		t.Fatalf("events: %v", qerr)
	}
	for _, row := range rows {
		if row.Event == auth.EventAdminRoleChanged && row.Email == "target@example.com" {
			if row.UserID == "" {
				t.Fatal("audit row has no target user id")
			}
			if row.ActorID != actorSess.User.ID {
				t.Fatalf("audit actor = %q, want %q", row.ActorID, actorSess.User.ID)
			}
			if row.Reason != "role=viewer" {
				t.Fatalf("audit reason = %q, want role=viewer", row.Reason)
			}
			return
		}
	}
	t.Fatalf("admin role change absent from audit rows: %+v", rows)
}

func TestCSVCellEscapesFormula(t *testing.T) {
	cases := map[string]string{
		"plain@example.com": "plain@example.com",
		"=cmd()":            "'=cmd()",
		"+1":                "'+1",
		"-2":                "'-2",
		"@SUM":              "'@SUM",
		"":                  "",
	}
	for in, want := range cases {
		if got := csvCell(in); got != want {
			t.Fatalf("csvCell(%q) = %q, want %q", in, got, want)
		}
	}
}
