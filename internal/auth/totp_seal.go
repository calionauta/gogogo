// SCOPE:core — TOTP secret encryption at rest
//
// A database read must not yield every user's second factor: sessions,
// magic-link tokens and recovery codes are all stored hashed, but TOTP
// verification needs the secret itself, so hashing is impossible and the
// answer is encryption. Secrets sealed with XChaCha20-Poly1305
// (x/crypto — no new dependency) under AUTH_ENC_KEY, stored as
// "v1:" + base64(nonce|ciphertext) in the same TEXT column: no migration,
// and legacy plaintext rows (no v1: prefix) keep verifying until the next
// successful code upgrades them in place (same shape as password
// rehash-on-login).
//
// No key configured (the default) keeps the legacy behavior — documented,
// not silent: the threat model (DB file readable = 2FA bypass) is stated
// in docs/configuration.md next to the variable. Fail-closed everywhere
// else: a sealed row without a key never verifies, and a wrong key never
// verifies.
package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
)

// totpSealVersion prefixes every sealed secret. Legacy rows (plain base32,
// no prefix) verify as before and upgrade on next success when a key is set.
const totpSealVersion = "v1:"

// sealTOTPSecret encrypts a fresh secret. Nonce is random per seal and
// prepended to the ciphertext; the whole envelope is raw base64.
func sealTOTPSecret(key *[32]byte, secret string) (string, error) {
	aead, err := chacha20poly1305.NewX(key[:])
	if err != nil {
		return "", fmt.Errorf("auth: totp seal: %w", err)
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("auth: totp seal entropy: %w", err)
	}
	sealed := aead.Seal(nonce, nonce, []byte(secret), nil)
	return totpSealVersion + base64.RawStdEncoding.EncodeToString(sealed), nil
}

// openTOTPSecret returns the verifiable secret for a stored row. Sealed
// rows need the key (absent or wrong key fails closed); legacy rows pass
// through with sealed=false so the caller can upgrade them after a
// successful verify.
func openTOTPSecret(key *[32]byte, stored string) (secret string, sealed bool, err error) {
	if !strings.HasPrefix(stored, totpSealVersion) {
		if stored == "" {
			return "", false, errors.New("auth: empty totp secret")
		}
		return stored, false, nil
	}
	if key == nil {
		return "", true, errors.New("auth: sealed totp secret without key")
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(stored, totpSealVersion))
	if err != nil {
		return "", true, fmt.Errorf("auth: totp seal decode: %w", err)
	}
	aead, err := chacha20poly1305.NewX(key[:])
	if err != nil {
		return "", true, fmt.Errorf("auth: totp seal: %w", err)
	}
	if len(raw) < chacha20poly1305.NonceSizeX {
		return "", true, errors.New("auth: truncated totp seal")
	}
	plain, err := aead.Open(nil, raw[:chacha20poly1305.NonceSizeX], raw[chacha20poly1305.NonceSizeX:], nil)
	if err != nil {
		return "", true, errors.New("auth: sealed totp secret rejected")
	}
	return string(plain), true, nil
}
