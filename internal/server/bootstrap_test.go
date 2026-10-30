package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// TestBootstrapSignupOpensOnlyWithoutAccounts is the first-run door for every
// frontend, not just the desktop: production defaults close signup, so an
// empty database must still allow user #1.
func TestBootstrapSignupOpensOnlyWithoutAccounts(t *testing.T) {
	sqlDB, err := db.OpenAt(filepath.Join(t.TempDir(), "boot.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("migrate: %v", merr)
	}
	q := dbgen.New(sqlDB)

	cfg := &config.Config{SignupEnabled: false}
	bootstrapSignup(cfg, q)
	if !cfg.SignupEnabled {
		t.Fatal("empty database left signup closed: first run is a locked door")
	}

	now := time.Now().UTC().Format(dbschema.TimeFormat)
	if _, cerr := q.CreateUser(context.Background(), dbgen.CreateUserParams{
		ID: dbschema.NewID(), Email: "first@example.test", PasswordHash: "!",
		Name: "First", Created: now, Updated: now,
	}); cerr != nil {
		t.Fatalf("create user: %v", cerr)
	}

	again := &config.Config{SignupEnabled: false}
	bootstrapSignup(again, q)
	if again.SignupEnabled {
		t.Fatal("signup reopened with an account present")
	}
}
