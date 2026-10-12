// SCOPE:core — Boot helper tests (admin promotion).
package server

import (
	"context"
	"testing"

	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
)

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
	if merr := db.Migrate(sqlDB); merr != nil {
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
