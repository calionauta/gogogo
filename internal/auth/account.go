// SCOPE:core — Account-management operations shared by the JSON API and the
// /account page. Both paths must enforce the same proof and revocation order.
package auth

import (
	"context"
	"fmt"
)

// ChangePassword verifies the current password, validates the new one,
// revokes every session and pending email token, then opens a replacement
// session. The caller owns the cookie.
func (a *Auth) ChangePassword(ctx context.Context, userID, current, next string) (string, error) {
	row, err := a.q.GetUser(ctx, userID)
	if err != nil {
		return "", ErrBadCredentials
	}
	if ok, _ := verifyPassword(row.PasswordHash, current); !ok {
		return "", ErrBadCredentials
	}
	if verr := validatePassword(next); verr != nil {
		return "", verr
	}
	if rerr := a.RevokeAllSessions(ctx, userID); rerr != nil {
		return "", rerr
	}
	if serr := a.SetPassword(ctx, userID, next); serr != nil {
		return "", serr
	}
	return a.openSession(ctx, userID)
}

// DisableTOTPWithPassword removes the second factor only after a fresh
// password proof, then revokes old sessions and opens a replacement.
func (a *Auth) DisableTOTPWithPassword(ctx context.Context, userID, password string) (string, error) {
	row, err := a.q.GetUser(ctx, userID)
	if err != nil {
		return "", ErrBadCredentials
	}
	if ok, _ := verifyPassword(row.PasswordHash, password); !ok {
		return "", ErrBadCredentials
	}
	if rerr := a.RevokeAllSessions(ctx, userID); rerr != nil {
		return "", rerr
	}
	if derr := a.DisableTOTP(ctx, userID); derr != nil {
		return "", derr
	}
	return a.openSession(ctx, userID)
}

// VerifyUserPassword is the read-only proof used before destructive account
// operations that do not rotate the session.
func (a *Auth) VerifyUserPassword(ctx context.Context, userID, password string) bool {
	row, err := a.q.GetUser(ctx, userID)
	if err != nil {
		return false
	}
	ok, _ := verifyPassword(row.PasswordHash, password)
	return ok
}

// UserByID is the small read the account page needs for verification/TOTP
// status without exposing the generated row shape to the UI layer.
func (a *Auth) UserByID(ctx context.Context, userID string) (email string, verified bool, totpEnabled bool, err error) {
	row, err := a.q.GetUser(ctx, userID)
	if err != nil {
		return "", false, false, fmt.Errorf("auth: account user: %w", err)
	}
	return row.Email, row.EmailVerifiedAt != "", row.TotpVerified != 0, nil
}
