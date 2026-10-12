// SCOPE:core — Demo-seed contract tests.
//
// The login form promises two demo accounts (demo1 prefilled, demo2 for
// the second browser); the legacy seed created both, and the flat port
// initially seeded only demo1. These tests pin the pair: both log in
// after Ensure, and re-running Ensure is a silent no-op (idempotent
// Signup — duplicate emails answer ErrBadCredentials, not an error).
package demo

import (
	"context"
	"testing"

	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

func seedAuth(t *testing.T) *auth.Auth {
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
	return a
}

// TestEnsureSeedsBothDemoAccounts pins the fresh-install contract: after
// one Ensure, both advertised pairs log in.
func TestEnsureSeedsBothDemoAccounts(t *testing.T) {
	a := seedAuth(t)
	ctx := context.Background()
	Ensure(ctx, a)

	for _, tc := range []struct{ email, name string }{
		{Email, "demo1"},
		{Email2, "demo2"},
	} {
		if _, _, err := a.Login(ctx, tc.email, Password); err != nil {
			t.Errorf("%s login after Ensure: %v", tc.name, err)
		}
	}
}

// TestEnsureIsIdempotent pins the repeat-boot path: a second Ensure must
// not error and must not disturb the existing sessions.
func TestEnsureIsIdempotent(t *testing.T) {
	a := seedAuth(t)
	ctx := context.Background()
	Ensure(ctx, a)
	Ensure(ctx, a)

	if _, _, err := a.Login(ctx, Email, Password); err != nil {
		t.Fatalf("demo1 login after double Ensure: %v", err)
	}
}
