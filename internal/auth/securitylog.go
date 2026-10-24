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

	"github.com/calionauta/gogogo/internal/observability"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// Security event names. Stable strings: a log search is a contract with the
// next operator, not an implementation detail.
const (
	evLoginRejected       = "login rejected"
	evSignupRejected      = "signup rejected"
	evSecondFactorBad     = "second factor rejected"
	evSecondFactorBlock   = "second factor throttled"
	evMagicRejected       = "magic link rejected"
	evMagicConsumed       = "magic link consumed"
	evSessionExpired      = "session expired"
	evSessionOrphan       = "session owner gone"
	evSessionRevoked      = "session revoked"
	evSessionsRevoked     = "all sessions revoked"
	evSignupSucceeded     = "signup"
	evLoginSucceeded      = "login succeeded"
	evPasswordChanged     = "password changed"
	evPasswordChangeNo    = "password change rejected"
	evPasswordReset       = "password reset"
	evTOTPDisableNo       = "second factor removal rejected"
	evEmailVerified       = "email verified"
	evTOTPEnabled         = "second factor enabled"
	evRecoveryCodesIssued = "recovery codes issued"
	evTOTPDisabled        = "second factor disabled"

	// Operator actions. Exported because the admin handler is a different
	// package: the vocabulary still lives here, so the admin UI, the log
	// filter and the table cannot drift.
	EventAdminRoleChanged     = "admin role changed"
	EventAdminSessionsRevoked = "admin sessions revoked"
	EventStepUpRejected       = "step-up rejected"
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
	reasonEmailUnverified = "email not verified"
	reasonBadCode         = "wrong code"
	reasonBrowserMismatch = "browser mismatch"
)

// secReject logs a rejected authentication attempt. email is the address as
// submitted (never a hash: it is the spray-vs-target signal); userID is the
// resolved account, empty when it could not be resolved.
//
// Routed through internal/observability so the line carries the auth component
// tag and any logger attrs already on the request context.
func secReject(ctx context.Context, event, reason, email, userID string) {
	observability.Warn(ctx, "auth: "+event,
		append(observability.WithComponent("auth", "core"),
			"event", event, "reason", reason, "email", email, "userID", userID)...)
}

// secEvent logs a non-rejection security event (a link consumed, a session
// expired) at Info.
func secEvent(ctx context.Context, event, userID string) {
	observability.Info(ctx, "auth: "+event,
		append(observability.WithComponent("auth", "core"),
			"event", event, "userID", userID)...)
}

// secReject and secEvent (methods) keep the slog line and add the durable
// audit row. The free functions above remain the logging primitive so the
// securitylog contract tests stay independent of a database.
func (a *Auth) secReject(ctx context.Context, event, reason, email, userID string) {
	secReject(ctx, event, reason, email, userID)
	a.recordSecurityEvent(ctx, event, reason, email, userID, "")
}

func (a *Auth) secEvent(ctx context.Context, event, userID string) {
	secEvent(ctx, event, userID)
	a.recordSecurityEvent(ctx, event, "", "", userID, "")
}

// RecordAudit writes one operator-visible event. Admin handlers call it after
// a successful mutation so the audit panel answers "who changed this role,
// and when" — a question the slog line alone cannot answer. reason is a short
// outcome note; email/userID identify the TARGET and actorID the operator.
func (a *Auth) RecordAudit(ctx context.Context, event, reason, email, userID, actorID string) {
	observability.Info(ctx, "auth: "+event,
		append(observability.WithComponent("auth", "core"),
			"event", event, "reason", reason, "email", email, "userID", userID,
			"actorID", actorID)...)
	a.recordSecurityEvent(ctx, event, reason, email, userID, actorID)
}

// recordSecurityEvent is best-effort: an audit write must never turn a
// rejected login into a 500 or block an expired-session cleanup.
func (a *Auth) recordSecurityEvent(ctx context.Context, event, reason, email, userID, actorID string) {
	if a.q == nil {
		return
	}
	// A client disconnect cancels the request context, but the audit row is
	// exactly what must survive a dropped connection.
	if err := a.q.InsertSecurityEvent(context.WithoutCancel(ctx), dbgen.InsertSecurityEventParams{
		ID: dbschema.NewID(), Event: event, Reason: reason, Email: email,
		UserID: userID, ActorID: actorID, Created: dbschema.Now(),
	}); err != nil {
		observability.Warn(ctx, "auth: security event persist failed",
			append(observability.WithComponent("auth", "core"),
				"event", event, "error", err)...)
	}
}
