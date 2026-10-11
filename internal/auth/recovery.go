// SCOPE:core — TOTP recovery codes (the lost-device path)
//
// OWASP's MFA sheet requires a secure procedure for MFA reset: without
// recovery codes, a lost authenticator means a locked-out user and a
// support ticket — or worse, a social-engineering bypass invented on the
// spot. Ten single-use codes are minted at enrollment confirmation time,
// shown ONCE, stored as SHA-256, and consumed by DELETE. Any code verifies
// in the totp/login step, so recovery needs no separate endpoint.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
)

// recoveryCodeCount is the batch size per issuance. Ten survives several
// lost-device scares without making the list itself a credential store
// worth stealing (hashes only, single-use each).
const recoveryCodeCount = 10

// recoveryCodeLen is the entropy per code in bytes (96 bits — far past
// brute force, short enough to type from paper).
const recoveryCodeLen = 12

// recoveryGroup groups base32 in fours for transcription.
const recoveryGroup = 4

// formatRecoveryCode groups base32 in fours (XXXX-XXXX-XXXX-XXXX) so paper
// copies survive transcription.
func formatRecoveryCode(raw []byte) string {
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	var parts []string
	for i := 0; i < len(enc); i += recoveryGroup {
		end := min(i+recoveryGroup, len(enc))
		parts = append(parts, enc[i:end])
	}
	return strings.Join(parts, "-")
}

// IssueRecoveryCodes mints a fresh batch for an authenticated user and
// returns the plaintext codes exactly once — callers display and forget.
// Batches accumulate; RotateRecoveryCodes replaces. Callers that want
// rotation semantics call rotate explicitly (enrollment does).
func (a *Auth) IssueRecoveryCodes(ctx context.Context, userID string) ([]string, error) {
	if _, err := a.q.GetUser(ctx, userID); err != nil {
		return nil, ErrBadCredentials
	}
	now := db.Now()
	plain := make([]string, 0, recoveryCodeCount)
	for range recoveryCodeCount {
		raw := make([]byte, recoveryCodeLen)
		if _, err := rand.Read(raw); err != nil {
			return nil, fmt.Errorf("auth: recovery entropy: %w", err)
		}
		code := formatRecoveryCode(raw)
		sum := sha256.Sum256([]byte(code))
		if err := a.q.CreateRecoveryCode(ctx, dbgen.CreateRecoveryCodeParams{
			CodeHash: hex.EncodeToString(sum[:]), UserID: userID, Created: now,
		}); err != nil {
			return nil, fmt.Errorf("auth: recovery store: %w", err)
		}
		plain = append(plain, code)
	}
	return plain, nil
}

// RotateRecoveryCodes replaces the user's batch (call before Issue when
// rotation — not accumulation — is wanted).
func (a *Auth) RotateRecoveryCodes(ctx context.Context, userID string) ([]string, error) {
	if err := a.q.DeleteRecoveryCodesForUser(ctx, userID); err != nil {
		return nil, fmt.Errorf("auth: recovery prune: %w", err)
	}
	return a.IssueRecoveryCodes(ctx, userID)
}

// consumeRecoveryCode deletes a matching code row, reporting whether one
// existed. Wrong codes and right codes for other users both miss.
func (a *Auth) consumeRecoveryCode(ctx context.Context, userID, code string) bool {
	sum := sha256.Sum256([]byte(strings.TrimSpace(code)))
	n, err := a.q.ConsumeRecoveryCode(ctx, dbgen.ConsumeRecoveryCodeParams{
		CodeHash: hex.EncodeToString(sum[:]), UserID: userID,
	})
	if err != nil {
		return false
	}
	return n == 1
}

// verifySecondFactor accepts a TOTP code or a recovery code. Recovery
// consumption is part of verification: a used code never verifies twice.
func (a *Auth) verifySecondFactor(ctx context.Context, user dbgen.User, code string) bool {
	code = strings.TrimSpace(code)
	if user.TotpSecret != "" && user.TotpVerified != 0 {
		// TOTP path is time-bound; no extra import needed here.
		if verifyTOTPCode(user.TotpSecret, code) {
			a.totpAttempts.forget(user.ID)
			return true
		}
		if !a.totpAttempts.allow(user.ID) {
			return false
		}
	}
	if a.consumeRecoveryCode(ctx, user.ID, code) {
		a.totpAttempts.forget(user.ID)
		return true
	}
	return false
}

// verifyTOTPCode wraps verifyTOTP at time.Now for the factor paths.
func verifyTOTPCode(secret, code string) bool {
	return verifyTOTP(secret, code, nowUTC())
}

// nowUTC is a seam for tests to freeze the TOTP clock.
var nowUTC = func() time.Time { return time.Now().UTC() }
