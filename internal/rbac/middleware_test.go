// SCOPE:core — RBAC middleware contract tests.
package rbac

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

func TestSubjectOfPinsRoles(t *testing.T) {
	if got := subjectOf(nil, nil); got != RoleAnonymous {
		t.Errorf("nil session = %q, want anonymous", got)
	}
	if got := subjectOf(nil, context.DeadlineExceeded); got != RoleAnonymous {
		t.Errorf("session error = %q, want anonymous (fail closed)", got)
	}
	if got := subjectOf(&auth.ValidatedSession{}, nil); got != RoleUser {
		t.Errorf("empty role = %q, want user (never escalate, never lock out)", got)
	}
	if got := subjectOf(&auth.ValidatedSession{Role: "admin"}, nil); got != RoleAdmin {
		t.Errorf("admin = %q, want admin", got)
	}
	if got := subjectOf(&auth.ValidatedSession{Role: "viewer"}, nil); got != RoleViewer {
		t.Errorf("viewer = %q, want viewer (never flattened to user)", got)
	}
	if got := subjectOf(&auth.ValidatedSession{Role: "superuser"}, nil); got != RoleUser {
		t.Errorf("unknown role = %q, want user (least surprise)", got)
	}
	if got := subjectOf(&auth.ValidatedSession{Role: "Admin"}, nil); got != RoleUser {
		t.Errorf("case-variant role = %q, want user (exact match only)", got)
	}
}

// TestMiddlewareEnforcesPinsDecisions drives the wired path end to end:
// pass-through when disabled, 403s with the policy matrix when enabled,
// sessions resolving to roles (admin promoted via the users row, exactly
// as boot promotion does it).
func TestMiddlewareEnforcesPinsDecisions(t *testing.T) {
	ctx := context.Background()
	sqlDB, err := db.OpenAt(t.TempDir() + "/rbac-mw.db")
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
	e, err := NewEnforcer()
	if err != nil {
		t.Fatalf("enforcer: %v", err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	_, userTok, serr := a.Signup(ctx, "user@example.com", "Correct-horse-99!", "")
	if serr != nil {
		t.Fatalf("signup: %v", serr)
	}
	admin, adminTok, serr := a.Signup(ctx, "admin@example.com", "Correct-horse-99!", "")
	if serr != nil {
		t.Fatalf("admin signup: %v", serr)
	}
	q := dbgen.New(sqlDB)
	if err := q.SetUserRole(ctx, dbgen.SetUserRoleParams{
		Role: "admin", Updated: dbschema.Now(), Email: admin.Email,
	}); err != nil {
		t.Fatalf("promote: %v", err)
	}

	// SetUserRole above takes ID; promoteAdmins works by email — prove the
	// boot path promotes the same row the middleware then honors.
	if _, err := q.GetUser(ctx, admin.ID); err != nil {
		t.Fatalf("admin row: %v", err)
	}

	get := func(target, token string) int {
		t.Helper()
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if token != "" {
			// #nosec G124 — test-only request cookie; never traverses a network.
			req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		rec := httptest.NewRecorder()
		Middleware(a, e, true)(next).ServeHTTP(rec, req)
		return rec.Code
	}

	// Disabled is pass-through with the same call shape.
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/todo", nil)
	rec := httptest.NewRecorder()
	Middleware(a, e, false)(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("disabled anon /todo = %d, want pass-through 200", rec.Code)
	}

	if code := get("/todo", ""); code != http.StatusForbidden {
		t.Errorf("anon /todo = %d, want 403", code)
	}
	if code := get("/login", ""); code != http.StatusOK {
		t.Errorf("anon /login = %d, want 200 (public surface)", code)
	}
	if code := get("/todo", userTok); code != http.StatusOK {
		t.Errorf("user /todo = %d, want 200", code)
	}
	if code := get("/config", userTok); code != http.StatusForbidden {
		t.Errorf("user /config = %d, want 403 (admin-only)", code)
	}
	if code := get("/config", adminTok); code != http.StatusOK {
		t.Errorf("admin /config = %d, want 200", code)
	}
	if code := get("/config", "bogus-token"); code != http.StatusForbidden {
		t.Errorf("bogus session /config = %d, want 403", code)
	}
}

// TestMiddlewarePermissivePinsAuditMode pins the rollout posture:
// permissive=true lets a would-be deny through (for log-driven policy
// completion), permissive=false denies it. Red-proof: hard-code
// permissive false in the middleware and the first assertion goes red;
// hard-code true and the second goes red.
func TestMiddlewarePermissivePinsAuditMode(t *testing.T) {
	ctx := context.Background()
	sqlDB, err := db.OpenAt(t.TempDir() + "/rbac-permissive.db")
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
	e, err := NewEnforcer()
	if err != nil {
		t.Fatalf("enforcer: %v", err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	_, userTok, serr := a.Signup(ctx, "perm@example.com", "Correct-horse-99!", "")
	if serr != nil {
		t.Fatalf("signup: %v", serr)
	}
	drive := func(permissive bool) int {
		t.Helper()
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/config", nil)
		// #nosec G124 — test-only request cookie; never traverses a network.
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: userTok})
		rec := httptest.NewRecorder()
		MiddlewareWithPermissive(a, e, true, permissive)(next).ServeHTTP(rec, req)
		return rec.Code
	}
	if code := drive(true); code != http.StatusOK {
		t.Errorf("permissive user /config = %d, want audit-mode 200", code)
	}
	if code := drive(false); code != http.StatusForbidden {
		t.Errorf("enforcing user /config = %d, want 403", code)
	}
}
