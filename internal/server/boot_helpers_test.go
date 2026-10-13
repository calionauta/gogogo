// SCOPE:core — Boot helper tests (admin promotion).
package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
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
	n, err := promoteAdmins([]string{"boss@example.com", "ghost@example.com"}, q)
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
	n, err := promoteRole([]string{"watcher@example.com", "ghost@example.com"}, rbac.RoleViewer, q)
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
	if _, verr := promoteRole([]string{"boss2@example.com"}, rbac.RoleViewer, q); verr != nil {
		t.Fatalf("viewer promote: %v", verr)
	}
	if _, aerr := promoteAdmins([]string{"boss2@example.com"}, q); aerr != nil {
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
