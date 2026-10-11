// SCOPE:core — Owned TOTP (RFC 6238) for second-factor auth
//
// No dependency: TOTP is HMAC-SHA1 plus dynamic truncation plus base32,
// small enough to own and test against the RFC vectors. pquerna/otp (the
// usual pick) scored 0/10 maintained in 2026 — depending on it would trade
// 60 owned lines for an unmaintained surface on the login path.
//
// Secrets are 20 random bytes stored base32 (no padding) in users.totp_secret.
// totp_verified guards enrollment: a secret is only enforced after the user
// proves the device once, so an abandoned setup never locks anyone out.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // SHA-1 is the RFC 6238 digest, not a security choice.
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strconv"
	"time"
)

// totpStep is the code validity window; totpDigits the code length. Both
// match Google Authenticator defaults so any standard app enrolls.
const (
	totpStep   = 30 * time.Second
	totpDigits = 6
	// totpMod is 10^digits: the truncation that turns 31 bits into a code.
	totpMod = 1000000
)

// totpSecretLen is the RFC 4226 recommended secret size.
const totpSecretLen = 20

// newTOTPSecret returns a fresh enrollment secret, base32-encoded.
func newTOTPSecret() (string, error) {
	raw := make([]byte, totpSecretLen)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("auth: totp entropy: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

// totpCode computes the 6-digit code for a secret at time t (RFC 6238 §4:
// counter = floor(t/30), HMAC-SHA1, dynamic truncation, mod 10^6).
func totpCode(secret string, t time.Time) (string, error) {
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return "", fmt.Errorf("auth: totp secret decode: %w", err)
	}
	steps := t.Unix() / int64(totpStep.Seconds())
	if steps < 0 {
		return "", fmt.Errorf("auth: totp time before epoch: %v", t)
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(steps))
	mac := hmac.New(sha1.New, raw)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", code%totpMod), nil
}

// verifyTOTP accepts the current step plus one step of clock skew either
// way. Comparison is constant-time; the window (not the code) absorbs
// slow typing and drifting clocks.
func verifyTOTP(secret, code string, now time.Time) bool {
	if len(code) != totpDigits {
		return false
	}
	if _, err := strconv.Atoi(code); err != nil {
		return false
	}
	for _, skew := range []int64{-1, 0, 1} {
		want, err := totpCode(secret, now.Add(time.Duration(skew)*totpStep))
		if err != nil {
			return false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// totpProvisioningURL builds the otpauth:// URL authenticator apps scan.
// Format per the (de-facto standard) Google Authenticator Key URI spec.
func totpProvisioningURL(account, issuer, secret string) string {
	return fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&digits=%d&period=%d",
		issuer, account, secret, issuer, totpDigits, int(totpStep.Seconds()))
}
