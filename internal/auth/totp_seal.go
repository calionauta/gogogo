// SCOPE:core — TOTP secret encryption at rest
//
// A database read must not yield any user's second factor: sessions,
// magic-link tokens and recovery codes are all stored hashed, and TOTP
// secrets are sealed with XChaCha20-Poly1305 (x/crypto — no new
// dependency) under a 32-byte key, stored as "v1:" +
// base64(nonce|ciphertext) in the same TEXT column. Non-sealed rows never
// verify (fail closed) — there is no plaintext fallback.
//
// The key comes from AUTH_ENC_KEY when set, else from an auto-created
// 0600 file (see LoadOrCreateKeyFile): encryption is always on, with no
// operator action. A lost key means re-enrollment via recovery codes,
// never silent plaintext.
package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
)

// totpSealVersion prefixes every sealed secret.
const totpSealVersion = "v1:"

// totpKeyFileName is the auto-created key file inside the app data dir.
const totpKeyFileName = "totp_seal.key"

// keyFilePerm is the key file mode (group/others must not read it);
// keyDirPerm the containing dir mode for auto-creation.
const (
	keyFilePerm = 0o600
	keyDirPerm  = 0o755
)

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

// openTOTPSecret returns the verifiable secret for a stored row. Anything
// that is not a sealed v1: envelope fails closed — including rows written
// before sealing existed (re-enroll via recovery codes) and any key
// problem (absent or wrong key).
func openTOTPSecret(key *[32]byte, stored string) (string, error) {
	if !strings.HasPrefix(stored, totpSealVersion) {
		return "", errors.New("auth: unsealed totp secret rejected")
	}
	if key == nil {
		return "", errors.New("auth: sealed totp secret without key")
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(stored, totpSealVersion))
	if err != nil {
		return "", fmt.Errorf("auth: totp seal decode: %w", err)
	}
	aead, err := chacha20poly1305.NewX(key[:])
	if err != nil {
		return "", fmt.Errorf("auth: totp seal: %w", err)
	}
	if len(raw) < chacha20poly1305.NonceSizeX {
		return "", errors.New("auth: truncated totp seal")
	}
	plain, err := aead.Open(nil, raw[:chacha20poly1305.NonceSizeX], raw[chacha20poly1305.NonceSizeX:], nil)
	if err != nil {
		return "", errors.New("auth: sealed totp secret rejected")
	}
	return string(plain), nil
}

// LoadOrCreateKeyFile returns the 32-byte seal key at dir/totp_seal.key,
// creating it (0600) on first boot. The key lives outside the database so
// a database read (dump, backup, copy) never yields second factors; losing
// the file means re-enrollment via recovery codes. A present-but-malformed
// file fails fast instead of silently minting a second key next to it.
func LoadOrCreateKeyFile(dir string) ([32]byte, error) {
	var zero [32]byte
	path := filepath.Join(dir, totpKeyFileName)
	raw, err := os.ReadFile(path)
	if err == nil {
		if len(raw) != totpKeyLen {
			return zero, fmt.Errorf("auth: totp key file %s holds %d bytes, want %d", path, len(raw), totpKeyLen)
		}
		var k [32]byte
		copy(k[:], raw)
		return k, nil
	}
	if !os.IsNotExist(err) {
		return zero, fmt.Errorf("auth: totp key file: %w", err)
	}
	var k [32]byte
	if _, err := rand.Read(k[:]); err != nil {
		return zero, fmt.Errorf("auth: totp key entropy: %w", err)
	}
	if err := os.MkdirAll(dir, keyDirPerm); err != nil {
		return zero, fmt.Errorf("auth: totp key dir: %w", err)
	}
	// #nosec G306 — keyFilePerm (0600) is the point: group/others must not read it.
	if err := os.WriteFile(path, k[:], keyFilePerm); err != nil {
		return zero, fmt.Errorf("auth: totp key write: %w", err)
	}
	return k, nil
}
