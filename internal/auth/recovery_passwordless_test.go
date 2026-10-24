// SCOPE:core — passwordless recovery rotation through the HTTP route.
package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

func TestTOTPRecoveryPasswordlessCode(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	if err := a.SetTOTPEncKey(make([]byte, 32)); err != nil {
		t.Fatalf("key: %v", err)
	}
	now := dbschema.Now()
	user, err := a.q.CreateUser(ctx, dbgen.CreateUserParams{
		ID: dbschema.NewID(), Email: "recovery-passwordless@example.com", PasswordHash: "!",
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
	token := "passwordless-recovery-token"
	if serr := a.q.CreateSession(ctx, dbgen.CreateSessionParams{
		TokenHash: tokenHash(token), UserID: user.ID, SessionID: dbschema.NewID(),
		LastSeen: now, Expires: time.Now().UTC().Add(time.Hour).Format(dbschema.TimeFormat),
		Created: now,
	}); serr != nil {
		t.Fatalf("create session: %v", serr)
	}
	mux := securityMux(a)
	// #nosec G124 — test-only request cookie.
	cookie := &http.Cookie{Name: CookieName, Value: token}
	bad := postJSON(t, mux, "/api/auth/totp/recovery", map[string]string{"code": "000000"}, cookie)
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("wrong-code recovery = %d, want 401", bad.Code)
	}
	good := postJSON(t, mux, "/api/auth/totp/recovery", map[string]string{"code": code}, cookie)
	if good.Code != http.StatusOK {
		t.Fatalf("code recovery = %d (%s)", good.Code, good.Body.String())
	}
}
