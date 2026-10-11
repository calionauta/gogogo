// SCOPE:core — Auth on limen (chi middleware + session cookie)
//
// Package auth owns password identity for the flat stack: limen core with the
// credential/password plugin, backed by the Turso database through limen's
// database/sql adapter. It replaces PocketBase's auth collection.
//
// Table layout: limen manages its own tables (`limen_users`, `limen_sessions`,
// plus its default `accounts`, `verifications`, `rate_limits`), created by
// migration 0002. They sit beside — never inside — the app tables from 0001
// (`users`, `sessions`, `todos`), which keep their sqlc owners. The rename is
// deliberate: limen's `users`/`sessions` carry limen's columns, so sharing the
// names would corrupt both owners.
//
// The session cookie keeps the app's historic name (`gogogo_auth`, HttpOnly +
// SameSite=Lax) so the browser surface and the SSE/realtime auth path are
// unchanged.
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/thecodearcher/limen"
	sqladapter "github.com/thecodearcher/limen/adapters/sql"
	credentialpassword "github.com/thecodearcher/limen/plugins/credential-password"
)

// CookieName is the app's session cookie. Kept distinct from any provider
// cookie on purpose: one cookie, one session, no clobbering.
const CookieName = "gogogo_auth"

// Auth is the limen instance plus the chi surface the app mounts. Build it
// once at boot and share it; limen is safe for concurrent use.
type Auth struct {
	limen *limen.Limen
}

// SecretLen is limen's required signing-secret size. Shorter secrets are
// rejected at boot (fail fast) rather than weakening every session.
const SecretLen = 32

// New builds the auth stack on an already-open, already-migrated *sql.DB
// (see internal/db: Open + Migrate, which applies the limen tables too).
// secret must be 32 bytes and stable across restarts — rotating it logs
// everybody out. secureCookies is false in dev (plain HTTP) and true in
// production.
func New(sqlDB *sql.DB, secret []byte, secureCookies bool) (*Auth, error) {
	if len(secret) != SecretLen {
		return nil, fmt.Errorf("auth: secret must be 32 bytes, got %d", len(secret))
	}
	l, err := limen.New(&limen.Config{
		BaseURL:  "http://localhost:8080",
		Database: sqladapter.NewSQLite(sqlDB),
		Secret:   secret,
		Plugins: []limen.Plugin{
			credentialpassword.New(),
		},
		HTTP: limen.NewDefaultHTTPConfig(
			limen.WithHTTPBasePath("/api/auth"),
			limen.WithHTTPSessionCookieName(CookieName),
			limen.WithHTTPCookieSecure(secureCookies),
		),
		Schema: limen.NewDefaultSchemaConfig(
			limen.WithSchemaUser(limen.WithUserTableName("limen_users")),
			limen.WithSchemaSession(limen.WithSessionTableName("limen_sessions")),
			limen.WithSchemaAccount(limen.WithAccountTableName("limen_accounts")),
			limen.WithSchemaVerification(limen.WithVerificationTableName("limen_verifications")),
			limen.WithSchemaRateLimit(limen.WithRateLimitTableName("limen_rate_limits")),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("auth: limen init: %w", err)
	}
	return &Auth{limen: l}, nil
}

// Handler serves limen's auth API (signup/signin/signout under /api/auth).
// Mount it at the chi root: r.Mount("/", a.Handler()).
func (a *Auth) Handler() http.Handler {
	return a.limen.Handler()
}

// ctxKey carries the validated session. Private so only SessionFrom reads it.
type ctxKey struct{}

// RequireSession is chi middleware: requests without a valid session get
// 401, the rest reach the handler with the session in context.
func (a *Auth) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := a.limen.GetSession(r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, sess)))
	})
}

// Session validates the request's session cookie and returns it. Handlers
// that need the current user resolve the owner id from sess.User.ID.
func (a *Auth) Session(r *http.Request) (*limen.ValidatedSession, error) {
	return a.limen.GetSession(r)
}

// SessionFrom returns the session RequireSession stored in the context.
func SessionFrom(ctx context.Context) (*limen.ValidatedSession, error) {
	sess, ok := ctx.Value(ctxKey{}).(*limen.ValidatedSession)
	if !ok || sess == nil {
		return nil, errors.New("auth: no session in context")
	}
	return sess, nil
}
