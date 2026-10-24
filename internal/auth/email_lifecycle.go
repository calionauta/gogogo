// SCOPE:core — Email verification and password-reset lifecycle.
//
// Both reuse the single-use, hashed magic_tokens table with a purpose tag,
// so a login link can never be replayed as a reset link. Delivery is an
// injected Mailer; the template wires SMTP when configured and leaves the
// endpoints closed (503) otherwise.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// Mailer delivers a plain-text message. Implementations must reject CR/LF in
// the recipient and subject (header injection).
type Mailer func(to, subject, body string) error

const (
	emailVerifyTTL   = 24 * time.Hour
	passwordResetTTL = 30 * time.Minute
)

// SetMailer installs the verification/reset delivery callback.
func (a *Auth) SetMailer(m Mailer) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mailer = m
}

func (a *Auth) mailerFn() Mailer {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.mailer
}

// SetRequireEmailVerification gates password login on a verified address.
func (a *Auth) SetRequireEmailVerification(required bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requireEmailVerification = required
}

func (a *Auth) emailVerificationRequired() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.requireEmailVerification
}

// SetPasswordResetPage installs the set-a-new-password renderer.
func (a *Auth) SetPasswordResetPage(fn func(http.ResponseWriter, *http.Request, string, string, string)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.passwordResetPage = fn
}

func (a *Auth) passwordResetRenderer() func(http.ResponseWriter, *http.Request, string, string, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.passwordResetPage
}

// createEmailToken mints one purpose-scoped token, replacing older tokens of
// the same purpose for that user.
func (a *Auth) createEmailToken(ctx context.Context, userID, purpose string, ttl time.Duration) (string, error) {
	token, hash, err := newToken()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	_ = a.q.DeleteMagicTokensForUserPurpose(ctx, dbgen.DeleteMagicTokensForUserPurposeParams{
		UserID: userID, Purpose: purpose,
	})
	if err := a.q.CreateMagicToken(ctx, dbgen.CreateMagicTokenParams{
		TokenHash: hash, UserID: userID, Purpose: purpose,
		Expires: now.Add(ttl).Format(dbschema.TimeFormat),
		Created: now.Format(dbschema.TimeFormat),
	}); err != nil {
		return "", fmt.Errorf("auth: email token store: %w", err)
	}
	_ = a.q.DeleteExpiredMagicTokens(ctx, now.Format(dbschema.TimeFormat))
	return token, nil
}

// RequestEmailVerification sends a verification link for an authenticated
// user. An already-verified address is a no-op.
func (a *Auth) RequestEmailVerification(ctx context.Context, userID, email, baseURL string) error {
	mailer := a.mailerFn()
	if mailer == nil {
		return errors.New("auth: no mailer configured")
	}
	user, err := a.q.GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("auth: verification user: %w", err)
	}
	if user.EmailVerifiedAt != "" {
		return nil
	}
	addr, err := normalizeEmail(email)
	if err != nil {
		return err
	}
	token, err := a.createEmailToken(ctx, userID, purposeVerify, emailVerifyTTL)
	if err != nil {
		return err
	}
	link := strings.TrimRight(baseURL, "/") + "/api/auth/verify?token=" + token
	return mailer(addr, "Verify your email", "Open this link to verify your email:\n\n"+link+"\n")
}

// RequestEmailVerificationDefault uses the configured APP_URL.
func (a *Auth) RequestEmailVerificationDefault(ctx context.Context, email string) error {
	base := a.publicBaseURL()
	if base == "" {
		return errors.New("auth: APP_URL not configured")
	}
	return a.RequestEmailVerificationByEmail(ctx, email, base)
}

// RequestPasswordResetDefault uses the configured APP_URL.
func (a *Auth) RequestPasswordResetDefault(ctx context.Context, email string) error {
	base := a.publicBaseURL()
	if base == "" {
		return errors.New("auth: APP_URL not configured")
	}
	return a.RequestPasswordReset(ctx, email, base)
}

// RequestEmailVerificationByEmail is the public request path: unknown or
// already-verified addresses are silent, so it cannot enumerate accounts.
func (a *Auth) RequestEmailVerificationByEmail(ctx context.Context, email, baseURL string) error {
	if a.mailerFn() == nil {
		return errors.New("auth: no mailer configured")
	}
	addr, err := normalizeEmail(email)
	if err != nil {
		//nolint:nilerr // anti-enumeration: invalid shape is a silent success.
		return nil
	}
	row, err := a.q.GetUserByEmail(ctx, addr)
	if err != nil || row.EmailVerifiedAt != "" {
		//nolint:nilerr // anti-enumeration: unknown/verified addresses are silent.
		return nil
	}
	return a.RequestEmailVerification(ctx, row.ID, row.Email, baseURL)
}

// VerifyEmail consumes a verification token and marks the address verified.
func (a *Auth) VerifyEmail(ctx context.Context, token string) (string, error) {
	if token == "" {
		return "", ErrBadCredentials
	}
	row, err := a.q.GetMagicToken(ctx, tokenHash(token))
	if err != nil || row.Purpose != purposeVerify {
		return "", ErrBadCredentials
	}
	expires, err := time.Parse(dbschema.TimeFormat, row.Expires)
	if err != nil || time.Now().UTC().After(expires) {
		_ = a.q.DeleteMagicToken(ctx, row.TokenHash)
		return "", ErrBadCredentials
	}
	user, err := a.q.GetUser(ctx, row.UserID)
	if err != nil {
		return "", ErrBadCredentials
	}
	if err := a.q.SetEmailVerified(ctx, dbgen.SetEmailVerifiedParams{
		EmailVerifiedAt: dbschema.Now(), Updated: dbschema.Now(), ID: row.UserID,
	}); err != nil {
		return "", fmt.Errorf("auth: verify email: %w", err)
	}
	_ = a.q.DeleteMagicToken(ctx, row.TokenHash)
	return user.Email, nil
}

// RequestPasswordReset sends a reset link. Unknown addresses return nil and
// send nothing (anti-enumeration); the caller always answers 200.
func (a *Auth) RequestPasswordReset(ctx context.Context, email, baseURL string) error {
	mailer := a.mailerFn()
	if mailer == nil {
		return errors.New("auth: no mailer configured")
	}
	addr, err := normalizeEmail(email)
	if err != nil {
		//nolint:nilerr // anti-enumeration: invalid shape is a silent success.
		return nil
	}
	row, err := a.q.GetUserByEmail(ctx, addr)
	if err != nil {
		//nolint:nilerr // anti-enumeration: unknown address is a silent success.
		return nil
	}
	// Per-account cooldown, like the magic link, to bound reset-mail spam.
	if latest, lerr := a.q.LatestMagicTokenForUserPurpose(ctx, dbgen.LatestMagicTokenForUserPurposeParams{
		UserID: row.ID, Purpose: purposeReset,
	}); lerr == nil {
		if created, perr := time.Parse(dbschema.TimeFormat, latest.Created); perr == nil &&
			time.Since(created) < magicRequestCooldown {
			return nil
		}
	}
	token, err := a.createEmailToken(ctx, row.ID, purposeReset, passwordResetTTL)
	if err != nil {
		return err
	}
	link := strings.TrimRight(baseURL, "/") + "/api/auth/password/reset?token=" + token
	body := "Open this link to choose a new password:\n\n" + link +
		"\n\nIf you didn't request this, ignore it.\n"
	return mailer(addr, "Reset your password", body)
}

// LookupEmailToken resolves a purpose-scoped token without consuming it, for
// the reset form's GET. It never deletes.
func (a *Auth) LookupEmailToken(ctx context.Context, token, purpose string) (string, bool) {
	if token == "" {
		return "", false
	}
	row, err := a.q.GetMagicToken(ctx, tokenHash(token))
	if err != nil || row.Purpose != purpose {
		return "", false
	}
	expires, err := time.Parse(dbschema.TimeFormat, row.Expires)
	if err != nil || time.Now().UTC().After(expires) {
		return "", false
	}
	user, err := a.q.GetUser(ctx, row.UserID)
	if err != nil {
		return "", false
	}
	return user.Email, true
}

// ResetPassword consumes a reset token, sets the new password, and revokes
// every existing session and pending email token.
func (a *Auth) ResetPassword(ctx context.Context, token, newPassword string) error {
	if token == "" {
		return ErrBadCredentials
	}
	if err := validatePassword(newPassword); err != nil {
		return err
	}
	row, err := a.q.GetMagicToken(ctx, tokenHash(token))
	if err != nil || row.Purpose != purposeReset {
		return ErrBadCredentials
	}
	expires, err := time.Parse(dbschema.TimeFormat, row.Expires)
	if err != nil || time.Now().UTC().After(expires) {
		_ = a.q.DeleteMagicToken(ctx, row.TokenHash)
		return ErrBadCredentials
	}
	// Revoke first: a failure leaves the user logged out with the old
	// password (safe) rather than rotating the secret while old sessions live.
	if err := a.RevokeAllSessions(ctx, row.UserID); err != nil {
		return err
	}
	return a.SetPassword(ctx, row.UserID, newPassword)
}

// mailboxAddress is a tiny guard used by SMTP implementations; kept here so
// the lifecycle tests can exercise the same parser.
func mailboxAddress(raw string) (string, error) {
	addr, err := mail.ParseAddress(raw)
	if err != nil {
		return "", err
	}
	return addr.Address, nil
}
