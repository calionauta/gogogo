// SCOPE:core — TOTP removal for passwordless (OAuth-only) rows.
package auth

import (
	"context"
	"testing"
	"time"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// TestDisableTOTPWithCodeForPasswordlessRow pins the OAuth-only path: such a
// row has no password, so the factor it removes (a current code) is the proof.
func TestDisableTOTPWithCodeForPasswordlessRow(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	if err := a.SetTOTPEncKey(make([]byte, 32)); err != nil {
		t.Fatalf("key: %v", err)
	}
	now := dbschema.Now()
	user, err := a.q.CreateUser(ctx, dbgen.CreateUserParams{
		ID: dbschema.NewID(), Email: "oauth-totp@example.com", PasswordHash: "!",
		Created: now, Updated: now,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	secret, _, _, err := a.BeginTOTP(ctx, user.ID)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	code, err := totpCode(secret, time.Now())
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	if verr := a.VerifyTOTPEnroll(ctx, user.ID, code); verr != nil {
		t.Fatalf("enroll: %v", verr)
	}
	if _, derr := a.DisableTOTPWithCode(ctx, user.ID, wrongTOTPCode(t, secret)); derr == nil {
		t.Fatal("wrong code removed the factor")
	}
	if _, derr := a.DisableTOTPWithCode(ctx, user.ID, code); derr != nil {
		t.Fatalf("disable: %v", derr)
	}
	row, err := a.q.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	if row.TotpVerified != 0 || row.TotpSecret != "" {
		t.Fatal("factor still present after a valid code")
	}
}
