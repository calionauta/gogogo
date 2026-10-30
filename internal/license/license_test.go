package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return pub, priv
}

func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv := newKey(t)
	token, err := Sign(priv, Claims{
		Sub: "user-1", Email: "a@b.test", Plan: "pro", SyncToken: "test-sync-token", ExpiresAt: 0,
	})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	c, err := Verify(pub, token, time.Now())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if c.Sub != "user-1" || c.Email != "a@b.test" || c.Plan != "pro" {
		t.Fatalf("claims = %+v", c)
	}
	if c.SyncToken != "test-sync-token" {
		t.Fatalf("SyncToken = %q, want test-sync-token", c.SyncToken)
	}
	if c.IssuedAt == 0 {
		t.Fatal("IssuedAt not stamped")
	}
}

func TestVerifyRejectsTamperedPayload(t *testing.T) {
	pub, priv := newKey(t)
	token, _ := Sign(priv, Claims{Plan: "pro"})
	// Flip one bit of the payload segment: the signature must not verify.
	parts := strings.Split(token, ".")
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	raw[0] ^= 0xff
	parts[1] = base64.RawURLEncoding.EncodeToString(raw)
	if _, err := Verify(pub, strings.Join(parts, "."), time.Now()); err == nil {
		t.Fatal("tampered payload verified")
	}
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	_, priv := newKey(t)
	otherPub, _ := newKey(t)
	token, _ := Sign(priv, Claims{Plan: "pro"})
	if _, err := Verify(otherPub, token, time.Now()); err == nil {
		t.Fatal("token verified under a different key")
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	pub, priv := newKey(t)
	token, _ := Sign(priv, Claims{ExpiresAt: time.Now().Add(-time.Hour).Unix()})
	if _, err := Verify(pub, token, time.Now()); err == nil {
		t.Fatal("expired token verified")
	}
}

func TestStoreActivatePersistsAndReloads(t *testing.T) {
	pub, priv := newKey(t)
	path := filepath.Join(t.TempDir(), "license.token")
	token, _ := Sign(priv, Claims{Sub: "u1", Plan: "pro"})

	st, err := Open(path, pub)
	if err != nil {
		t.Fatalf("open empty: %v", err)
	}
	if st.Valid(time.Now()) {
		t.Fatal("empty store reported valid")
	}
	if _, aerr := st.Activate(token, time.Now()); aerr != nil {
		t.Fatalf("activate: %v", aerr)
	}
	if !st.Valid(time.Now()) {
		t.Fatal("activated store not valid")
	}

	st2, err := Open(path, pub)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if !st2.Valid(time.Now()) || st2.Claims().Sub != "u1" {
		t.Fatalf("reloaded store = %+v", st2.Claims())
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("token file mode = %o, want 600", perm)
	}

	if err := st2.Clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if st2.Valid(time.Now()) {
		t.Fatal("cleared store still valid")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("token file survived Clear: %v", err)
	}
}

func TestPublicKeyParsing(t *testing.T) {
	pub, _ := newKey(t)
	if _, perr := ParsePublicKey(hex.EncodeToString(pub)); perr != nil {
		t.Fatalf("hex key: %v", perr)
	}
	if _, err := ParsePublicKey("not-a-key"); err == nil {
		t.Fatal("garbage key accepted")
	}
	if _, err := ParsePublicKey(""); err == nil {
		t.Fatal("empty key accepted")
	}
}
