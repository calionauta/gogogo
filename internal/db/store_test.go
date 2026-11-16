package db

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// openTestDB is the shared fixture: a migrated temp database via the
// package's own OpenAt (tursogo, like production) plus dbschema.Migrate.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := OpenAt(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := dbschema.Migrate(sqlDB); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return sqlDB
}

// newStore is the fixture for every generated-query test: a migrated temp
// database and a *dbgen.Queries built the same way a handler will build it.
func newStore(t *testing.T) *dbgen.Queries {
	t.Helper()
	return dbgen.New(openTestDB(t))
}

func createUser(t *testing.T, q *dbgen.Queries, email string) dbgen.User {
	t.Helper()
	now := dbschema.Now()
	u, err := q.CreateUser(context.Background(), dbgen.CreateUserParams{
		ID:           dbschema.NewID(),
		Email:        email,
		PasswordHash: "hash",
		Name:         "Demo",
		Created:      now,
		Updated:      now,
	})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", email, err)
	}
	return u
}

// TestGeneratedUserQueries covers the identity path including RETURNING, which
// is what lets a handler answer with the created row without a second read.
func TestGeneratedUserQueries(t *testing.T) {
	q := newStore(t)
	ctx := context.Background()
	created := createUser(t, q, "demo1@demo.app")

	got, err := q.GetUserByEmail(ctx, "demo1@demo.app")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if got.ID != created.ID || got.Email != created.Email {
		t.Fatalf("GetUserByEmail = %+v, want id %s", got, created.ID)
	}

	byID, err := q.GetUser(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if byID.Email != created.Email {
		t.Fatalf("GetUser email = %q, want %q", byID.Email, created.Email)
	}

	if uerr := q.UpdateUserPassword(ctx, dbgen.UpdateUserPasswordParams{
		PasswordHash: "new-hash", Updated: dbschema.Now(), ID: created.ID,
	}); uerr != nil {
		t.Fatalf("UpdateUserPassword: %v", uerr)
	}
	after, err := q.GetUser(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUser after update: %v", err)
	}
	if after.PasswordHash != "new-hash" {
		t.Fatalf("password hash = %q, want new-hash", after.PasswordHash)
	}

	n, err := q.CountUsers(ctx)
	if err != nil || n != 1 {
		t.Fatalf("CountUsers = %d, %v; want 1, nil", n, err)
	}

	if _, err := q.GetUserByEmail(ctx, "nobody@demo.app"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing user err = %v, want sql.ErrNoRows", err)
	}
}

// TestGeneratedSessionQueries covers the session path the auth package
// uses: create, look up by token hash, prune expired rows, revoke one,
// revoke all of a user's.
func TestGeneratedSessionQueries(t *testing.T) {
	q := newStore(t)
	ctx := context.Background()
	user := createUser(t, q, "demo1@demo.app").ID

	future := time.Now().UTC().Add(time.Hour).Format(dbschema.TimeFormat)
	past := time.Now().UTC().Add(-time.Hour).Format(dbschema.TimeFormat)

	if err := q.CreateSession(ctx, dbgen.CreateSessionParams{
		TokenHash: "hash-live", UserID: user, SessionID: "sid-live",
		LastSeen: dbschema.Now(), Expires: future, Created: dbschema.Now(),
	}); err != nil {
		t.Fatalf("CreateSession(live): %v", err)
	}
	if err := q.CreateSession(ctx, dbgen.CreateSessionParams{
		TokenHash: "hash-stale", UserID: user, SessionID: "sid-stale",
		LastSeen: dbschema.Now(), Expires: past, Created: dbschema.Now(),
	}); err != nil {
		t.Fatalf("CreateSession(stale): %v", err)
	}

	sess, err := q.GetSession(ctx, "hash-live")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.UserID != user {
		t.Fatalf("session user = %q, want %q", sess.UserID, user)
	}

	if err := q.DeleteExpiredSessions(ctx, dbschema.Now()); err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}
	if _, err := q.GetSession(ctx, "hash-stale"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("stale session still present: %v", err)
	}

	if err := q.DeleteSession(ctx, "hash-live"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := q.GetSession(ctx, "hash-live"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("revoked session still present: %v", err)
	}

	if err := q.CreateSession(ctx, dbgen.CreateSessionParams{
		TokenHash: "hash-2", UserID: user, SessionID: "sid-2",
		LastSeen: dbschema.Now(), Expires: future, Created: dbschema.Now(),
	}); err != nil {
		t.Fatalf("CreateSession(2): %v", err)
	}
	if err := q.DeleteUserSessions(ctx, user); err != nil {
		t.Fatalf("DeleteUserSessions: %v", err)
	}
	if _, err := q.GetSession(ctx, "hash-2"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("session survived DeleteUserSessions: %v", err)
	}
}

// TestOpenCreatesParentDir pins the boot behaviour a fresh clone depends on:
// `data/` does not exist yet, and Open has to create it before the engine
// tries to open a file inside it.
func TestOpenCreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", "app.db")
	sqlDB, err := OpenAt(path)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	// The contract is on the filesystem, not just the handle: the nested
	// dirs and the file must exist before Migrate touches them.
	if st, serr := os.Stat(path); serr != nil || st.IsDir() {
		t.Fatalf("OpenAt left no db file at %s: %v", path, serr)
	}
	if err := dbschema.Migrate(sqlDB); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
}
