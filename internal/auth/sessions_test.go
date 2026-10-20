// SCOPE:core — SessionByToken tests (the transport-free session path)
//
// Session validates cookies over *http.Request; SessionByToken resolves
// a raw token without a request — what cmd/gui's persisted "remember
// me" file needs. The legacy features/auth suite pinned the same
// round-trip through Login + ResolveOwner; these tests pin the flat
// equivalents: token → user, unknown token fails, and a revoked session
// (logout) fails too.
package auth

import (
	"context"
	"testing"

	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

func newSessionAuth(t *testing.T) *Auth {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/session-token.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("db migrate: %v", merr)
	}
	a, err := New(dbgen.New(sqlDB), false)
	if err != nil {
		t.Fatalf("auth new: %v", err)
	}
	return a
}

func TestSessionByTokenRoundTrip(t *testing.T) {
	a := newSessionAuth(t)
	ctx := context.Background()
	_, token, err := a.Signup(ctx, "token@example.com", "Correct-horse-99!", "Token")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	sess, err := a.SessionByToken(ctx, token)
	if err != nil {
		t.Fatalf("SessionByToken: %v", err)
	}
	if sess.User.Email != "token@example.com" || sess.User.ID == "" {
		t.Fatalf("resolved user = %+v, want the signed-up identity", sess.User)
	}
	if sess.Expires.IsZero() {
		t.Error("resolved session has no expiry")
	}
}

func TestSessionByTokenRejectsUnknownToken(t *testing.T) {
	a := newSessionAuth(t)
	if _, err := a.SessionByToken(context.Background(), "not-a-real-token"); err == nil {
		t.Fatal("SessionByToken accepted an unknown token")
	}
	if _, err := a.SessionByToken(context.Background(), ""); err == nil {
		t.Fatal("SessionByToken accepted an empty token")
	}
}

func TestSessionByTokenRejectsRevokedSession(t *testing.T) {
	a := newSessionAuth(t)
	ctx := context.Background()
	_, token, err := a.Signup(ctx, "revoked@example.com", "Correct-horse-99!", "Revoked")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	a.Logout(ctx, token)
	if _, err := a.SessionByToken(ctx, token); err == nil {
		t.Fatal("SessionByToken resolved a logged-out session (revocation not honored)")
	}
}
