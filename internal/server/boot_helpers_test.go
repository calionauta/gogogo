// SCOPE:core — Boot helper tests (admin promotion).
package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/demo"
	"github.com/calionauta/gogogo/internal/rbac"
)

// TestWireTOTPKeyPinsSealKeyRule pins the boot wiring without booting:
// env key wins, malformed env fails (never silently substituted), else the
// auto-created file, else keyless with enrollment failing closed downstream
// (wireTOTPKey itself succeeds — absence is not an error here).
func TestWireTOTPKeyPinsSealKeyRule(t *testing.T) {
	newAuth := func(t *testing.T) *auth.Auth {
		t.Helper()
		sqlDB, err := db.OpenAt(t.TempDir() + "/wire.db")
		if err != nil {
			t.Fatalf("db open: %v", err)
		}
		t.Cleanup(func() { sqlDB.Close() })
		if merr := dbschema.Migrate(sqlDB); merr != nil {
			t.Fatalf("db migrate: %v", merr)
		}
		a, err := auth.New(dbgen.New(sqlDB), false)
		if err != nil {
			t.Fatalf("auth new: %v", err)
		}
		return a
	}
	newCfg := func() *config.Config {
		cfg := config.Load()
		cfg.DataDir = t.TempDir()
		return cfg
	}
	t.Setenv("AUTH_ENC_KEY", "ab")
	if err := wireTOTPKey(newAuth(t), newCfg()); err == nil {
		t.Fatal("malformed env key accepted (must fail, never substitute)")
	}
	t.Setenv("AUTH_ENC_KEY", strings.Repeat("ab", 32))
	if err := wireTOTPKey(newAuth(t), newCfg()); err != nil {
		t.Fatalf("valid env key rejected: %v", err)
	}
	os.Unsetenv("AUTH_ENC_KEY")
	a := newAuth(t)
	cfg := newCfg()
	if err := wireTOTPKey(a, cfg); err != nil {
		t.Fatalf("auto file rejected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "totp_seal.key")); err != nil {
		t.Fatalf("auto file not created: %v", err)
	}
}

// TestPromoteAdminsPinsPromotionOnly pins the ADMIN_EMAILS contract:
// existing rows promote, unowned addresses skip silently (never provision,
// never fail the boot), and the promoted row carries the role the RBAC
// middleware honors.
func TestPromoteAdminsPinsPromotionOnly(t *testing.T) {
	ctx := context.Background()
	sqlDB, err := db.OpenAt(t.TempDir() + "/promote.db")
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
	user, _, serr := a.Signup(ctx, "boss@example.com", "Correct-horse-99!", "")
	if serr != nil {
		t.Fatalf("signup: %v", serr)
	}
	q := dbgen.New(sqlDB)
	n, err := promoteAdmins(ctx, []string{"boss@example.com", "ghost@example.com"}, q)
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if n != 1 {
		t.Fatalf("promoted = %d, want 1 (ghost skipped, not provisioned)", n)
	}
	row, err := q.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if row.Role != "admin" {
		t.Fatalf("role = %q, want admin", row.Role)
	}
}

// TestPromoteViewersPinsViewerContract mirrors the admin promotion pin:
// existing rows take the viewer role, ghosts skip silently, and an
// address in both lists ends admin (admin promotion runs last — ties
// escalate, never restrict, so a typo cannot lock an admin out).
func TestPromoteViewersPinsViewerContract(t *testing.T) {
	ctx := context.Background()
	sqlDB, err := db.OpenAt(t.TempDir() + "/promote-viewer.db")
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
	watcher, _, serr := a.Signup(ctx, "watcher@example.com", "Correct-horse-99!", "")
	if serr != nil {
		t.Fatalf("signup: %v", serr)
	}
	boss, _, serr := a.Signup(ctx, "boss2@example.com", "Correct-horse-99!", "")
	if serr != nil {
		t.Fatalf("signup: %v", serr)
	}
	q := dbgen.New(sqlDB)
	n, err := promoteRole(ctx, []string{"watcher@example.com", "ghost@example.com"}, rbac.RoleViewer, q)
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if n != 1 {
		t.Fatalf("promoted = %d, want 1 (ghost skipped, not provisioned)", n)
	}
	row, err := q.GetUser(ctx, watcher.ID)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if row.Role != rbac.RoleViewer {
		t.Fatalf("role = %q, want viewer", row.Role)
	}
	// Tie: viewer first, admin last — admin wins.
	if _, verr := promoteRole(ctx, []string{"boss2@example.com"}, rbac.RoleViewer, q); verr != nil {
		t.Fatalf("viewer promote: %v", verr)
	}
	if _, aerr := promoteAdmins(ctx, []string{"boss2@example.com"}, q); aerr != nil {
		t.Fatalf("admin promote: %v", aerr)
	}
	bossRow, err := q.GetUser(ctx, boss.ID)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if bossRow.Role != "admin" {
		t.Fatalf("tied role = %q, want admin (admin wins ties)", bossRow.Role)
	}
}

// TestApplyRBACWiresPermissive pins the boot wiring end to end: with
// enforcement on, a plain user's request to an admin-only path allows
// under permissive audit mode and denies when enforcing. The middleware
// unit test pins the decision; this pins that applyRBAC actually passes
// cfg.RBACPermissive through. Red-proof: hard-code false in applyRBAC
// and the audit case goes red.
func TestApplyRBACWiresPermissive(t *testing.T) {
	ctx := context.Background()
	sqlDB, err := db.OpenAt(t.TempDir() + "/apply-rbac.db")
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
	_, userTok, serr := a.Signup(ctx, "permboot@example.com", "Correct-horse-99!", "")
	if serr != nil {
		t.Fatalf("signup: %v", serr)
	}
	q := dbgen.New(sqlDB)
	drive := func(permissive bool) int {
		t.Helper()
		mux := http.NewServeMux()
		mux.Handle("/config", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		cfg := &config.Config{RBACEnabled: true, RBACPermissive: permissive}
		h, herr := applyRBAC(ctx, a, cfg, q, mux)
		if herr != nil {
			t.Fatalf("applyRBAC: %v", herr)
		}
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/config", nil)
		// #nosec G124 — test-only request cookie; never traverses a network.
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: userTok})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := drive(true); code != http.StatusOK {
		t.Errorf("permissive user /config via boot wiring = %d, want 200", code)
	}
	if code := drive(false); code != http.StatusForbidden {
		t.Errorf("enforcing user /config via boot wiring = %d, want 403", code)
	}
}

// TestDemoAdminOperatorOnlyInDev pins the demo/operator boundary: a
// development demo adds demo1 to the operator allowlist so the console and
// stats menus are demonstrable; a production demo (the public one) keeps the
// list empty and demo1 stays an admin without operator surfaces.
func TestDemoAdminOperatorOnlyInDev(t *testing.T) {
	for _, tc := range []struct {
		name string
		dev  bool
		want bool
	}{
		{"development", true, true},
		{"production", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sqlDB, err := db.OpenAt(t.TempDir() + "/demo-operator.db")
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
			cfg := &config.Config{Dev: tc.dev, DemoAccounts: true}
			demoQueries(cfg, a, sqlDB)
			got := slices.Contains(cfg.AdminEmails, demo.Email)
			if got != tc.want {
				t.Fatalf("demo1 in AdminEmails = %v, want %v (list %v)", got, tc.want, cfg.AdminEmails)
			}
		})
	}
}
