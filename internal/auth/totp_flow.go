// SCOPE:core — TOTP enrollment, verification and removal.
//
// The second factor is owned here, not in a plugin: BeginTOTP stores a sealed
// secret, VerifyTOTPEnroll proves the device before enforcement starts, and
// VerifyTOTPLogin opens the session the password step withheld. Removing the
// factor is a credential mutation, so callers gate it behind a fresh proof.
package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// BeginTOTP starts enrollment for an authenticated user: stores a fresh
// secret (unverified) and returns it with the provisioning URL their
// authenticator app scans. Nothing is enforced until VerifyTOTPEnroll.
func (a *Auth) BeginTOTP(ctx context.Context, userID string) (secret, url, email string, err error) {
	user, err := a.q.GetUser(ctx, userID)
	if err != nil {
		return "", "", "", ErrBadCredentials
	}
	// Sealing needs the key: enrollment without one fails closed here,
	// not later with a stored secret nobody can verify. Production always
	// configures it (AUTH_ENC_KEY or the auto-created key file).
	if a.totpKey == nil {
		return "", "", "", errors.New("auth: totp encryption key not configured")
	}
	secret, err = newTOTPSecret()
	if err != nil {
		return "", "", "", err
	}
	stored, err := sealTOTPSecret(a.totpKey, secret)
	if err != nil {
		return "", "", "", err
	}
	if err := a.q.SetTOTPSecret(ctx, dbgen.SetTOTPSecretParams{
		TotpSecret: stored, Updated: dbschema.Now(), ID: userID,
	}); err != nil {
		return "", "", "", fmt.Errorf("auth: totp begin: %w", err)
	}
	return secret, totpProvisioningURL(user.Email, "gogogo", secret), user.Email, nil
}

// VerifyTOTPEnroll confirms the device (proving the user scanned the
// secret) and enforces 2FA from then on.
func (a *Auth) VerifyTOTPEnroll(ctx context.Context, userID, code string) error {
	user, err := a.q.GetUser(ctx, userID)
	if err != nil {
		return ErrBadCredentials
	}
	if user.TotpSecret == "" {
		return ErrBadCredentials
	}
	opened, err := openTOTPSecret(a.totpKey, user.TotpSecret)
	if err != nil || !verifyTOTP(opened, code, time.Now()) {
		return ErrBadCredentials
	}
	if err := a.q.VerifyTOTPFlag(ctx, dbgen.VerifyTOTPFlagParams{
		Updated: dbschema.Now(), ID: userID,
	}); err != nil {
		return fmt.Errorf("auth: totp enroll: %w", err)
	}
	a.secEvent(ctx, evTOTPEnabled, userID)
	return nil
}

// VerifyTOTPLogin completes a password login pending second factor,
// opening the session the first step withheld. Accepts the current
// authenticator code or any unused recovery code (consumed on use).
func (a *Auth) VerifyTOTPLogin(ctx context.Context, userID, code string) (string, error) {
	user, err := a.q.GetUser(ctx, userID)
	if err != nil {
		return "", ErrBadCredentials
	}
	if user.TotpSecret == "" || user.TotpVerified == 0 {
		return "", ErrBadCredentials
	}
	if !a.verifySecondFactor(ctx, user, code) {
		secReject(ctx, evSecondFactorBad, reasonBadCode, user.Email, user.ID)
		return "", ErrBadCredentials
	}
	return a.openSession(ctx, user.ID)
}

// SetTOTPEncKey installs the at-rest encryption key for TOTP secrets
// (AUTH_ENC_KEY, 32 bytes). Empty clears back to keyless (enrollment and
// verification fail closed until a key is set).
// Wrong-sized keys fail fast: a truncated key must never silently weaken
// every second factor.
func (a *Auth) SetTOTPEncKey(key []byte) error {
	if len(key) == 0 {
		a.totpKey = nil
		return nil
	}
	if len(key) != totpKeyLen {
		return fmt.Errorf("auth: totp key must be %d bytes, got %d", totpKeyLen, len(key))
	}
	var k [totpKeyLen]byte
	copy(k[:], key)
	a.totpKey = &k
	return nil
}

// DisableTOTP removes second-factor enforcement. Callers gate this behind
// a fresh password check (the HTTP route does).
func (a *Auth) DisableTOTP(ctx context.Context, userID string) error {
	if err := a.q.ClearTOTP(ctx, dbgen.ClearTOTPParams{Updated: dbschema.Now(), ID: userID}); err != nil {
		return fmt.Errorf("auth: totp disable: %w", err)
	}
	return nil
}
