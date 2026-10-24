// SCOPE:core — shared step-up proof for account and admin mutations.
package auth

import (
	"context"
	"testing"
	"time"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

func TestVerifyStepUpPasswordPath(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	if err := a.SetTOTPEncKey(make([]byte, 32)); err != nil {
		t.Fatalf("key: %v", err)
	}
	user, _, err := a.Signup(ctx, "stepup@example.com", "Correct-horse-99!", "")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	if ok, verr := a.VerifyStepUp(ctx, user.ID, "wrong"); verr != nil || ok {
		t.Fatalf("wrong password accepted: ok=%v err=%v", ok, verr)
	}
	if ok, verr := a.VerifyStepUp(ctx, user.ID, "Correct-horse-99!"); verr != nil || !ok {
		t.Fatalf("password rejected: ok=%v err=%v", ok, verr)
	}
	secret, _, _, err := a.BeginTOTP(ctx, user.ID)
	if err != nil {
		t.Fatalf("begin totp: %v", err)
	}
	code, err := totpCode(secret, time.Now())
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	if verr := a.VerifyTOTPEnroll(ctx, user.ID, code); verr != nil {
		t.Fatalf("enroll: %v", verr)
	}
	if ok, verr := a.VerifyStepUp(ctx, user.ID, code); verr != nil || ok {
		t.Fatalf("password account accepted a code: ok=%v err=%v", ok, verr)
	}
}

func TestVerifyStepUpCodeForPasswordlessRow(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	if err := a.SetTOTPEncKey(make([]byte, 32)); err != nil {
		t.Fatalf("key: %v", err)
	}
	now := dbschema.Now()
	user, err := a.q.CreateUser(ctx, dbgen.CreateUserParams{
		ID: dbschema.NewID(), Email: "oauth-stepup@example.com", PasswordHash: "!",
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
	if ok, verr := a.VerifyStepUp(ctx, user.ID, "000000"); verr != nil || ok {
		t.Fatalf("wrong code accepted: ok=%v err=%v", ok, verr)
	}
	if ok, verr := a.VerifyStepUp(ctx, user.ID, code); verr != nil || !ok {
		t.Fatalf("code rejected: ok=%v err=%v", ok, verr)
	}
}

func TestRotateRecoveryCodesWithCodeForPasswordlessRow(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	if err := a.SetTOTPEncKey(make([]byte, 32)); err != nil {
		t.Fatalf("key: %v", err)
	}
	now := dbschema.Now()
	user, err := a.q.CreateUser(ctx, dbgen.CreateUserParams{
		ID: dbschema.NewID(), Email: "oauth-recovery@example.com", PasswordHash: "!",
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
	if _, rerr := a.RotateRecoveryCodesWithCode(ctx, user.ID, "000000"); rerr == nil {
		t.Fatal("wrong code rotated recovery codes")
	}
	codes, rerr := a.RotateRecoveryCodesWithCode(ctx, user.ID, code)
	if rerr != nil {
		t.Fatalf("rotate: %v", rerr)
	}
	if len(codes) == 0 {
		t.Fatal("no recovery codes issued")
	}
}
