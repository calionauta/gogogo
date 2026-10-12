// SCOPE:core — TOTP at-rest encryption tests (seal, upgrade, fail-closed).
package auth

import (
	"context"
	"crypto/rand"
	"strings"
	"testing"
	"time"
)

func testKey(t *testing.T) [32]byte {
	t.Helper()
	var k [32]byte
	if _, err := rand.Read(k[:]); err != nil {
		t.Fatalf("key entropy: %v", err)
	}
	return k
}

// TestTOTPSealedRoundTrip pins the encrypted path end to end: with a key
// configured, the stored row is sealed (never the plaintext secret), and
// enroll + login verify through the seal.
func TestTOTPSealedRoundTrip(t *testing.T) {
	ctx := context.Background()
	a := testAuth(t, testDB(t))
	k := testKey(t)
	if err := a.SetTOTPEncKey(k[:]); err != nil {
		t.Fatalf("key: %v", err)
	}
	if err := a.SetTOTPEncKey([]byte("short")); err == nil {
		t.Fatal("truncated key accepted")
	}
	user, _, serr := a.Signup(ctx, "seal@example.com", "Correct-horse-99!", "")
	if serr != nil {
		t.Fatalf("signup: %v", serr)
	}
	secret, _, _, berr := a.BeginTOTP(ctx, user.ID)
	if berr != nil {
		t.Fatalf("begin: %v", berr)
	}
	row, err := a.q.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if !strings.HasPrefix(row.TotpSecret, totpSealVersion) {
		t.Fatalf("stored secret = %q, want sealed v1:", row.TotpSecret)
	}
	if strings.Contains(row.TotpSecret, secret) {
		t.Fatal("stored seal contains the plaintext secret")
	}
	code, _ := totpCode(secret, time.Now())
	if err := a.VerifyTOTPEnroll(ctx, user.ID, code); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if _, required, err := a.Login(ctx, "seal@example.com", "Correct-horse-99!"); err != nil || !required {
		t.Fatalf("enforced login = %v, required=%v", err, required)
	}
	code, _ = totpCode(secret, time.Now())
	if _, err := a.VerifyTOTPLogin(ctx, user.ID, code); err != nil {
		t.Fatalf("sealed login: %v", err)
	}
}

// TestTOTPLegacyUpgradesOnVerify pins the migration without a migration:
// a plaintext row from before the key verifies once, then rewrites itself
// sealed (flag preserved — a sealed upgrade must never disenroll 2FA).
func TestTOTPLegacyUpgradesOnVerify(t *testing.T) {
	ctx := context.Background()
	a := testAuth(t, testDB(t))
	user, _, serr := a.Signup(ctx, "legacy@example.com", "Correct-horse-99!", "")
	if serr != nil {
		t.Fatalf("signup: %v", serr)
	}
	secret, _, _, berr := a.BeginTOTP(ctx, user.ID)
	if berr != nil {
		t.Fatalf("begin: %v", berr)
	}
	code, _ := totpCode(secret, time.Now())
	if err := a.VerifyTOTPEnroll(ctx, user.ID, code); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	row, _ := a.q.GetUser(ctx, user.ID)
	if strings.HasPrefix(row.TotpSecret, totpSealVersion) {
		t.Fatal("no key configured, yet the row is sealed")
	}
	k := testKey(t)
	if err := a.SetTOTPEncKey(k[:]); err != nil {
		t.Fatalf("key: %v", err)
	}
	code, _ = totpCode(secret, time.Now())
	if _, err := a.VerifyTOTPLogin(ctx, user.ID, code); err != nil {
		t.Fatalf("legacy login: %v", err)
	}
	row, _ = a.q.GetUser(ctx, user.ID)
	if !strings.HasPrefix(row.TotpSecret, totpSealVersion) {
		t.Fatalf("row still plaintext after verify, want sealed: %q", row.TotpSecret)
	}
	if row.TotpVerified == 0 {
		t.Fatal("upgrade reset the verified flag (2FA disenrolled)")
	}
	code, _ = totpCode(secret, time.Now())
	if _, err := a.VerifyTOTPLogin(ctx, user.ID, code); err != nil {
		t.Fatalf("post-upgrade login: %v", err)
	}
}

// TestTOTPSealFailClosed pins both closed directions: a sealed row without
// a key never verifies, and the wrong key never verifies (no oracle beyond
// the shared bad-credentials error).
func TestTOTPSealFailClosed(t *testing.T) {
	ctx := context.Background()
	a := testAuth(t, testDB(t))
	k := testKey(t)
	if err := a.SetTOTPEncKey(k[:]); err != nil {
		t.Fatalf("key: %v", err)
	}
	user, _, _ := a.Signup(ctx, "sealed2@example.com", "Correct-horse-99!", "")
	secret, _, _, _ := a.BeginTOTP(ctx, user.ID)
	code, _ := totpCode(secret, time.Now())
	if err := a.VerifyTOTPEnroll(ctx, user.ID, code); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	other := testAuth(t, testDB(t))
	row, _ := a.q.GetUser(ctx, user.ID)
	// Same row, no key: closed.
	if _, _, err := openTOTPSecret(nil, row.TotpSecret); err == nil {
		t.Fatal("sealed row opened without a key")
	}
	// Same row, wrong key: closed. (A second Auth sharing no state stands
	// in for a restarted process with a rotated/lost key.)
	wrong := testKey(t)
	if err := other.SetTOTPEncKey(wrong[:]); err != nil {
		t.Fatalf("key: %v", err)
	}
	if _, _, err := openTOTPSecret(other.totpKey, row.TotpSecret); err == nil {
		t.Fatal("sealed row opened with the wrong key")
	}
}
