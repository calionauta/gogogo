// SCOPE:core — security-event vocabulary for the owned auth stack
//
// internal/auth logged NOTHING before this file existed: a failed login, a
// TOTP guess, an expired magic link and a revoked session were all invisible.
// That is not a missing metric, it is a missing incident response — with a
// brute-force run against an account there was no evidence to point at
// afterwards, in a package the security audit had already listed as a
// residual risk.
//
// The vocabulary lives here, in one place, so a log filter written against
// these strings cannot silently desync from a typo at a call site.
//
// ## What may and may not be logged
//
// NEVER, by construction: the password, the session token, the TOTP code,
// the recovery code, the magic-link token. The call sites pass identifiers
// and outcome reasons only.
//
// ALWAYS: the outcome and, when it is known, the account identifier
// (userID). The attempted email is logged on a rejected login — it is the
// only signal that separates a targeted account from a password spray, and
// the anti-enumeration contract that hides this from the HTTP response is
// about what the caller learns, not about what the operator can see.
//
// Levels: Warn for rejected attempts (the interesting case), Info for the
// normal lifecycle (a link consumed, a session expired). A level filter must
// find attacks without drowning in routine traffic.
package auth

import (
	"context"
	"log/slog"
)

// Security event names. Stable strings: a log search is a contract with the
// next operator, not an implementation detail.
const (
	evLoginRejected     = "login rejected"
	evSignupRejected    = "signup rejected"
	evSecondFactorBad   = "second factor rejected"
	evSecondFactorBlock = "second factor throttled"
	evMagicRejected     = "magic link rejected"
	evMagicConsumed     = "magic link consumed"
	evSessionExpired    = "session expired"
	evSessionOrphan     = "session owner gone"
)

// Rejected-attempt reasons. Also stable strings, for the same reason.
const (
	reasonUnknownAccount  = "unknown account"
	reasonBadPassword     = "bad password"
	reasonUnknownToken    = "unknown token"
	reasonExpiredToken    = "expired token"
	reasonThrottled       = "attempt budget exhausted"
	reasonSigninThrottled = "signin budget exhausted"
	reasonSignupThrottled = "signup budget exhausted"
	reasonBadCode         = "wrong code"
	reasonBrowserMismatch = "browser mismatch"
)

// secReject logs a rejected authentication attempt. email is the address as
// submitted (never a hash: it is the spray-vs-target signal); userID is the
// resolved account, empty when it could not be resolved.
//
// Deliberately not routed through internal/observability: that helper is used
// by three files and provides a tracing seam nothing consumes yet, while the
// rest of the flat stack calls log/slog directly. Consistency with the code
// that is actually live beats consistency with the code that is not — see
// docs/architecture-decision.md.
func secReject(ctx context.Context, event, reason, email, userID string) {
	slog.WarnContext(ctx, "auth: "+event,
		"event", event, "reason", reason, "email", email, "userID", userID)
}

// secEvent logs a non-rejection security event (a link consumed, a session
// expired) at Info.
func secEvent(ctx context.Context, event, userID string) {
	slog.InfoContext(ctx, "auth: "+event,
		"event", event, "userID", userID)
}
