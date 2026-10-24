// SCOPE:core — Account-management operations shared by the JSON API and the
// /account page. Both paths must enforce the same proof and revocation order.
package auth

import (
	"context"
	"fmt"
	"strings"
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
		a.secReject(ctx, evPasswordChangeNo, reasonBadPassword, row.Email, userID)
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
	a.secEvent(ctx, evPasswordChanged, userID)
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
		a.secReject(ctx, evTOTPDisableNo, reasonBadPassword, row.Email, userID)
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

// DisableTOTPWithCode removes the second factor after proving a current
// authenticator or recovery code. It is the passwordless path: an OAuth-only
// row has no password, and a session alone must not be able to downgrade the
// factor it just satisfied.
func (a *Auth) DisableTOTPWithCode(ctx context.Context, userID, code string) (string, error) {
	user, err := a.q.GetUser(ctx, userID)
	if err != nil {
		return "", ErrBadCredentials
	}
	if !a.verifySecondFactor(ctx, user, code) {
		a.secReject(ctx, evSecondFactorBad, reasonBadCode, user.Email, user.ID)
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

// VerifyStepUp accepts a password when the account has one, or a current
// authenticator/recovery code when it has a verified factor. It is the shared
// proof for destructive owner actions and admin mutations. A store failure is
// an error; a wrong proof is (false, nil) so a caller cannot turn one into the
// other.
func (a *Auth) VerifyStepUp(ctx context.Context, userID, proof string) (bool, error) {
	row, err := a.q.GetUser(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("auth: step-up lookup: %w", err)
	}
	if strings.HasPrefix(row.PasswordHash, "argon2id$v=") {
		ok, _ := verifyPassword(row.PasswordHash, proof)
		return ok, nil
	}
	if row.TotpSecret != "" && row.TotpVerified != 0 {
		return a.verifySecondFactor(ctx, row, proof), nil
	}
	return false, nil
}

// RotateRecoveryCodesWithCode is the passwordless recovery path: the caller
// proves the factor it is rotating with a current authenticator/recovery code.
func (a *Auth) RotateRecoveryCodesWithCode(ctx context.Context, userID, code string) ([]string, error) {
	user, err := a.q.GetUser(ctx, userID)
	if err != nil {
		return nil, ErrBadCredentials
	}
	if !a.verifySecondFactor(ctx, user, code) {
		a.secReject(ctx, evSecondFactorBad, reasonBadCode, user.Email, user.ID)
		return nil, ErrBadCredentials
	}
	return a.RotateRecoveryCodes(ctx, userID)
}

// RevokeAllSessionsWithProof is sign-out-everywhere with the strongest proof
// the account has: the password when one is set, a current authenticator/
// recovery code when TOTP is enrolled, or the session itself for an OAuth-only
// row with no factor (the only thing that can prove it).
func (a *Auth) RevokeAllSessionsWithProof(ctx context.Context, userID, proof string) error {
	row, err := a.q.GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("auth: revoke lookup: %w", err)
	}
	switch {
	case strings.HasPrefix(row.PasswordHash, "argon2id$v="):
		if ok, _ := verifyPassword(row.PasswordHash, proof); !ok {
			return ErrBadCredentials
		}
	case row.TotpSecret != "" && row.TotpVerified != 0:
		if !a.verifySecondFactor(ctx, row, proof) {
			return ErrBadCredentials
		}
	}
	return a.RevokeAllSessions(ctx, userID)
}

// CanStepUp reports whether the account has a proof that VerifyStepUp can
// accept: a password or a verified TOTP factor. OAuth-only rows without TOTP
// cannot perform admin mutations until they enroll a factor or set a password.
func (a *Auth) CanStepUp(ctx context.Context, userID string) (bool, error) {
	row, err := a.q.GetUser(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("auth: step-up state: %w", err)
	}
	return strings.HasPrefix(row.PasswordHash, "argon2id$v=") ||
		(row.TotpSecret != "" && row.TotpVerified != 0), nil
}

// HasPassword reports whether the account can prove a password. OAuth-only
// rows carry the "!" sentinel (see oauth.go): a destructive action that asks
// for a password must not lock those users out of their own account.
func (a *Auth) HasPassword(ctx context.Context, userID string) (bool, error) {
	row, err := a.q.GetUser(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("auth: password state: %w", err)
	}
	return strings.HasPrefix(row.PasswordHash, "argon2id$v="), nil
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
