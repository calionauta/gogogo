// SCOPE:core — Passwordless magic-link logins
//
// A magic link is a single-use random token delivered out-of-band (email).
// Requesting never reveals whether the address exists; verifying consumes
// the row by DELETE, so replay is impossible by construction. No account is
// provisioned on request — magic links log existing users in; signup stays
// explicit (password or OAuth), so the endpoint cannot be used to spam
// arbitrary addresses with new accounts.
//
// Delivery is an injected callback: the template has no opinion on SMTP vs
// API senders, and tests inject a fake. Fifteen minutes is the expiry: long
// enough for slow inboxes, short enough to bound the window.
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"time"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// magicLinkTTL bounds a login link's lifetime (10–15 minutes is the
// industry consensus: slow inboxes still deliver, interception windows
// stay small).
const magicLinkTTL = 15 * time.Minute

// magicRequestCooldown is the quiet period between two magic links for
// one account. It bounds link-email spam through this endpoint (an
// attacker requesting links for a known address can only trigger one
// mail per minute); legitimate users waiting on slow inboxes already hold
// a valid 15-minute link. Enforced silently, like everything else here.
const magicRequestCooldown = time.Minute

// MagicSender delivers the login link. Implementations send email; tests
// capture it.
type MagicSender func(to, link string) error

// RequestMagicLink starts a passwordless login for email. Unknown addresses
// still return nil (no account enumeration) — they just receive nothing,
// and no account is provisioned: passwordless signup is an explicit per-app
// choice, not a spam vector in the template default. The link embeds the raw
// token; only its hash is stored.
//
// bind is the browser-binding value the HTTP handler also stores in a
// short-lived cookie. It travels in the link so a link forwarded to another
// browser cannot open a session there (login CSRF). Empty omits it; the
// handler is the only production caller and always supplies one.
func (a *Auth) RequestMagicLink(
	ctx context.Context, email, baseURL, bind string, send MagicSender,
) error {
	// Invalid shape and unknown addresses fall through to the same silent
	// nil at the end: the anti-enumeration contract. Only genuine failures
	// (no entropy, no storage, no sender) return errors.
	addr, err := normalizeEmail(email)
	var row dbgen.User
	if err == nil {
		row, err = a.q.GetUserByEmail(ctx, addr)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("auth: magic lookup: %w", err)
		}
	}
	//nolint:nilerr // silent nil is the anti-enumeration contract: invalid
	// shape and unknown addresses succeed exactly like known ones.
	if err != nil {
		return nil
	}
	token, hash, err := newToken()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if latest, err := a.q.LatestMagicTokenForUser(ctx, row.ID); err == nil {
		if created, perr := time.Parse(dbschema.TimeFormat, latest.Created); perr == nil &&
			now.Sub(created) < magicRequestCooldown {
			return nil // cooling down: the live link still works.
		}
	}
	_ = a.q.DeleteMagicTokensForUser(ctx, row.ID)
	if err := a.q.CreateMagicToken(ctx, dbgen.CreateMagicTokenParams{
		TokenHash: hash, UserID: row.ID,
		Expires: now.Add(magicLinkTTL).Format(dbschema.TimeFormat),
		Created: now.Format(dbschema.TimeFormat),
	}); err != nil {
		return fmt.Errorf("auth: magic store: %w", err)
	}
	_ = a.q.DeleteExpiredMagicTokens(ctx, now.Format(dbschema.TimeFormat))
	if send == nil {
		return errors.New("auth: no magic sender configured")
	}
	q := url.Values{"token": {token}}
	if bind != "" {
		q.Set("bind", bind)
	}
	return send(addr, baseURL+"/api/auth/magic?"+q.Encode())
}

// VerifyMagicLink consumes a login token and opens a session. Consumed by
// DELETE: a used or unknown token simply misses, so replay and guessing
// share one indistinguishable error.
func (a *Auth) VerifyMagicLink(ctx context.Context, token string) (string, error) {
	if token == "" {
		return "", ErrBadCredentials
	}
	row, err := a.q.GetMagicToken(ctx, tokenHash(token))
	if err != nil {
		secReject(ctx, evMagicRejected, reasonUnknownToken, "", "")
		return "", ErrBadCredentials
	}
	expires, err := time.Parse(dbschema.TimeFormat, row.Expires)
	if err != nil || time.Now().UTC().After(expires) {
		_ = a.q.DeleteMagicToken(ctx, row.TokenHash)
		secReject(ctx, evMagicRejected, reasonExpiredToken, "", row.UserID)
		return "", ErrBadCredentials
	}
	_ = a.q.DeleteMagicToken(ctx, row.TokenHash)
	secEvent(ctx, evMagicConsumed, row.UserID)
	return a.openSession(ctx, row.UserID)
}
