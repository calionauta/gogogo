// SCOPE:core — TOTP at-rest encryption tests (seal, key file, fail-closed).
package auth

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
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

// TestTOTPSealedRoundTrip pins the encrypted path end to end: the stored
// row is sealed (never the plaintext secret), and enroll + login verify
// through the seal.
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

// TestBeginTOTPRequiresKey pins the fail-closed enrollment: without a seal
// key there is no enroll (a stored secret nobody can verify would brick
// the user). Production always configures one; tests that need enrollment
// set it explicitly (pluginAuth does).
func TestBeginTOTPRequiresKey(t *testing.T) {
	ctx := context.Background()
	a := testAuth(t, testDB(t))
	user, _, serr := a.Signup(ctx, "nokey@example.com", "Correct-horse-99!", "")
	if serr != nil {
		t.Fatalf("signup: %v", serr)
	}
	if _, _, _, err := a.BeginTOTP(ctx, user.ID); err == nil {
		t.Fatal("enrollment without a seal key accepted")
	}
}

// TestTOTPSealFailClosed pins both closed directions on one enrolled row:
// clearing the key fails verification, and the wrong key fails
// verification (no oracle beyond the shared bad-credentials error).
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
	if err := a.SetTOTPEncKey(nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	code, _ = totpCode(secret, time.Now())
	if _, err := a.VerifyTOTPLogin(ctx, user.ID, code); err == nil {
		t.Fatal("sealed row verified without a key")
	}
	wrong := testKey(t)
	if err := a.SetTOTPEncKey(wrong[:]); err != nil {
		t.Fatalf("key: %v", err)
	}
	if _, err := a.VerifyTOTPLogin(ctx, user.ID, code); err == nil {
		t.Fatal("sealed row verified with the wrong key")
	}
	if err := a.SetTOTPEncKey(k[:]); err != nil {
		t.Fatalf("key: %v", err)
	}
	if _, err := a.VerifyTOTPLogin(ctx, user.ID, code); err != nil {
		t.Fatalf("right key rejected: %v", err)
	}
}

// TestLoadOrCreateKeyFile pins the zero-operator-action key lifecycle:
// create once (0600), stable across boots, malformed fails fast (never a
// silent second key next to the broken one).
func TestLoadOrCreateKeyFile(t *testing.T) {
	dir := t.TempDir()
	k1, err := LoadOrCreateKeyFile(dir)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	st, err := os.Stat(filepath.Join(dir, totpKeyFileName))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("key file perm = %o, want 600", st.Mode().Perm())
	}
	k2, err := LoadOrCreateKeyFile(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if k1 != k2 {
		t.Fatal("key file not stable across loads")
	}
	if err := os.WriteFile(filepath.Join(dir, totpKeyFileName), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateKeyFile(dir); err == nil {
		t.Fatal("malformed key file accepted")
	}
}
