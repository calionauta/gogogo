// SCOPE:core — Demo-seed contract tests.
//
// The login form promises two demo accounts (demo1 prefilled, demo2 for
// the second browser) and, since the roles landed, a fixed permission
// split: demo1 admin, demo2 viewer. These tests pin the pair, the roles,
// and idempotence (a repeat Ensure is a silent no-op for signup and a
// role refresh).
package demo

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

func seedAuth(t *testing.T) (*auth.Auth, *dbgen.Queries) {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/demo-seed.db")
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
	return a, dbgen.New(sqlDB)
}

// TestEnsureSeedsBothDemoAccounts pins the fresh-install contract: after
// one Ensure, both advertised pairs log in.
func TestEnsureSeedsBothDemoAccounts(t *testing.T) {
	a, q := seedAuth(t)
	ctx := context.Background()
	Ensure(ctx, a, q)

	for _, tc := range []struct{ email, name string }{
		{Email, "demo1"},
		{Email2, "demo2"},
	} {
		if _, _, err := a.Login(ctx, tc.email, Password); err != nil {
			t.Errorf("%s login after Ensure: %v", tc.name, err)
		}
	}
}

// TestEnsureSeedsDemoRoles pins the permission split the login card
// advertises: demo1 is promoted to admin, demo2 to the restricted viewer
// role. Red-proof: swap the two role strings and this fails.
func TestEnsureSeedsDemoRoles(t *testing.T) {
	a, q := seedAuth(t)
	ctx := context.Background()
	Ensure(ctx, a, q)

	for _, tc := range []struct{ email, want string }{
		{Email, "admin"},
		{Email2, "viewer"},
	} {
		row, err := q.GetUserByEmail(ctx, tc.email)
		if err != nil {
			t.Fatalf("get %s: %v", tc.email, err)
		}
		if row.Role != tc.want {
			t.Errorf("%s role = %q, want %q", tc.email, row.Role, tc.want)
		}
	}
}

// TestEnsureIsIdempotent pins the repeat-boot path: a second Ensure must
// not error, must not disturb existing sessions, and must not flip roles.
func TestEnsureIsIdempotent(t *testing.T) {
	a, q := seedAuth(t)
	ctx := context.Background()
	Ensure(ctx, a, q)
	Ensure(ctx, a, q)

	if _, _, err := a.Login(ctx, Email, Password); err != nil {
		t.Fatalf("demo1 login after double Ensure: %v", err)
	}
	row, err := q.GetUserByEmail(ctx, Email2)
	if err != nil {
		t.Fatalf("get demo2: %v", err)
	}
	if row.Role != "viewer" {
		t.Errorf("demo2 role after double Ensure = %q, want viewer", row.Role)
	}
}

// TestDisableClosesPublishedCredentials pins the production default: a
// deployment that did not opt into the demo seed must not leave the known
// pair usable after an upgrade. Red-proof: drop the password disable and
// Login succeeds (the account was seeded before the flag changed).
func TestDisableClosesPublishedCredentials(t *testing.T) {
	a, q := seedAuth(t)
	ctx := context.Background()
	Ensure(ctx, a, q)

	// A live session and a pending magic link are credentials too.
	token, _, err := a.Login(ctx, Email, Password)
	if err != nil {
		t.Fatalf("demo login before disable: %v", err)
	}
	Disable(ctx, q)

	if _, _, err := a.Login(ctx, Email, Password); err == nil {
		t.Fatal("published demo credentials still log in after Disable")
	}
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	// #nosec G124 — test-only request cookie.
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	if _, err := a.Session(req); err == nil {
		t.Fatal("demo session survived Disable")
	}
	for _, email := range []string{Email, Email2} {
		row, gerr := q.GetUserByEmail(ctx, email)
		if gerr != nil {
			t.Fatalf("get %s: %v", email, gerr)
		}
		if row.PasswordHash != "!" || row.Role != roleUser {
			t.Errorf("%s = hash %q role %q, want disabled user", email, row.PasswordHash, row.Role)
		}
	}
}

// TestEnsureRestoresDisabledPair pins the opt-in round trip: turning
// DEMO_ACCOUNTS_ENABLED back on must restore the published password, not
// leave the "!" marker in place (the row already exists, so Signup alone
// cannot repair it).
func TestEnsureRestoresDisabledPair(t *testing.T) {
	a, q := seedAuth(t)
	ctx := context.Background()
	Ensure(ctx, a, q)
	Disable(ctx, q)
	Ensure(ctx, a, q)
	for _, tc := range []struct{ email, role string }{
		{Email, roleAdmin},
		{Email2, roleViewer},
	} {
		if _, _, err := a.Login(ctx, tc.email, Password); err != nil {
			t.Errorf("%s login after re-enable: %v", tc.email, err)
		}
		row, err := q.GetUserByEmail(ctx, tc.email)
		if err != nil {
			t.Fatalf("get %s: %v", tc.email, err)
		}
		if row.Role != tc.role {
			t.Errorf("%s role = %q, want %q", tc.email, row.Role, tc.role)
		}
	}
}
