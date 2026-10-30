// SCOPE:core — Offline entitlement tokens (license / account activation).
//
// One signed token format serves both activation paths described in
// docs/research/desktop-auth-sync-2026-10.md:
//
//   - LICENSE_MODE=license: the product owner mints a token offline with
//     `cmd/license mint` and sends it to the buyer.
//   - LICENSE_MODE=account: the server (or the owner) mints the same token
//     bound to a user id (`sub`), so activation can adopt that identity.
//
// Verification is local and offline: Ed25519 over a compact JSON payload.
// A token is `gogogo1.<base64url(payload)>.<base64url(sig)>`. There is no
// phone-home and no hardware fingerprint: this is deterrence, not DRM.
package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// tokenPrefix versions the wire format so a future change can reject old
// tokens explicitly instead of parsing them as something else.
const tokenPrefix = "gogogo1"

const (
	tokenFileMode = 0o600 // bearer credential
	tokenDirMode  = 0o750
)

// ErrNoLicense means no token is stored (or it was cleared).
var ErrNoLicense = errors.New("license: no activation token")

// Claims is the signed payload. Zero values are omitted, so a perpetual
// license (ExpiresAt=0) carries no exp field at all.
type Claims struct {
	Sub       string `json:"sub,omitempty"`
	Email     string `json:"email,omitempty"`
	Plan      string `json:"plan,omitempty"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp,omitempty"`
	// SyncURL is the NATS leaf-node endpoint this entitlement may use. It
	// rides in the token so "connect to cloud" cannot be pointed at an
	// arbitrary host by the end user.
	SyncURL string `json:"sync,omitempty"`
}

// Sign returns the wire token for claims.
func Sign(priv ed25519.PrivateKey, c Claims) (string, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return "", errors.New("license: bad private key size")
	}
	if c.IssuedAt == 0 {
		c.IssuedAt = time.Now().UTC().Unix()
	}
	payload, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("license: marshal claims: %w", err)
	}
	sig := ed25519.Sign(priv, payload)
	return tokenPrefix + "." + b64.EncodeToString(payload) + "." + b64.EncodeToString(sig), nil
}

// Verify checks the signature and the expiry, returning the claims. A
// malformed token, a tampered payload and an expired token are distinct
// errors so the activation page can explain which one happened.
func Verify(pub ed25519.PublicKey, raw string, now time.Time) (*Claims, error) {
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) != 3 || parts[0] != tokenPrefix {
		return nil, errors.New("license: malformed token")
	}
	payload, err := b64.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("license: malformed payload")
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil {
		return nil, errors.New("license: malformed signature")
	}
	if len(pub) != ed25519.PublicKeySize {
		return nil, errors.New("license: no public key configured")
	}
	if !ed25519.Verify(pub, payload, sig) {
		return nil, errors.New("license: signature does not verify")
	}
	var c Claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, errors.New("license: unreadable claims")
	}
	if c.ExpiresAt > 0 && now.UTC().Unix() > c.ExpiresAt {
		return nil, fmt.Errorf("license: expired on %s", time.Unix(c.ExpiresAt, 0).UTC().Format(time.RFC3339))
	}
	return &c, nil
}

// ParsePublicKey accepts the key as 64 hex chars or standard base64. Hex is
// what `cmd/license keygen` prints, so copy-paste into LICENSE_PUBLIC_KEY
// cannot silently mean something else.
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("license: empty public key")
	}
	if raw, err := hex.DecodeString(s); err == nil && len(raw) == ed25519.PublicKeySize {
		return ed25519.PublicKey(raw), nil
	}
	if raw, err := b64.DecodeString(s); err == nil && len(raw) == ed25519.PublicKeySize {
		return ed25519.PublicKey(raw), nil
	}
	return nil, errors.New("license: public key must be 32 bytes as hex or base64")
}

// ParsePrivateKey reads the 64-byte private key that keygen writes (hex).
func ParsePrivateKey(s string) (ed25519.PrivateKey, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		return nil, errors.New("license: private key must be 64 bytes as hex")
	}
	return ed25519.PrivateKey(raw), nil
}

// Store is the activation state: the verified claims plus the token on disk.
// One store is shared by the gate (reads) and the activation handler (writes),
// hence the mutex.
type Store struct {
	mu     sync.RWMutex
	pub    ed25519.PublicKey
	path   string
	raw    string
	claims *Claims
}

// Open loads the token at path, if any. A missing file is not an error: that
// is simply an unactivated app. An unreadable or invalid file is ignored
// (the gate stays closed) and the reason is returned for the boot log.
func Open(path string, pub ed25519.PublicKey) (*Store, error) {
	s := &Store{pub: pub, path: path}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return s, fmt.Errorf("license: read %s: %w", path, err)
	}
	c, verr := Verify(pub, string(raw), time.Now())
	if verr != nil {
		return s, fmt.Errorf("license: stored token rejected: %w", verr)
	}
	s.raw, s.claims = strings.TrimSpace(string(raw)), c
	return s, nil
}

// Valid reports whether a non-expired token is loaded.
func (s *Store) Valid(now time.Time) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.claims == nil {
		return false
	}
	return s.claims.ExpiresAt == 0 || now.UTC().Unix() <= s.claims.ExpiresAt
}

// Claims returns the loaded claims (nil when unactivated).
func (s *Store) Claims() *Claims {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.claims
}

// Token returns the stored wire token (empty when unactivated).
func (s *Store) Token() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.raw
}

// Activate verifies and persists a token, then makes it live. The file is
// 0600: the token is a bearer credential for the entitlement and, in account
// mode, carries the user id.
func (s *Store) Activate(raw string, now time.Time) (*Claims, error) {
	c, err := Verify(s.pub, raw, now)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), tokenDirMode); err != nil {
		return nil, fmt.Errorf("license: create dir: %w", err)
	}
	if err := os.WriteFile(s.path, []byte(strings.TrimSpace(raw)), tokenFileMode); err != nil {
		return nil, fmt.Errorf("license: persist token: %w", err)
	}
	s.mu.Lock()
	s.raw, s.claims = strings.TrimSpace(raw), c
	s.mu.Unlock()
	return c, nil
}

// Clear removes the stored token (deactivation).
func (s *Store) Clear() error {
	s.mu.Lock()
	s.raw, s.claims = "", nil
	s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("license: clear token: %w", err)
	}
	return nil
}

var b64 = base64.RawURLEncoding
