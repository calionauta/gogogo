// SCOPE:core — Flat auth sessions (validate, middleware, cookies)
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/calionauta/gogogo/internal/db"
)

// Session validates the request's session cookie. Expired or unknown
// sessions fail closed; callers redirect to /login.
func (a *Auth) Session(r *http.Request) (*ValidatedSession, error) {
	cookie, err := r.Cookie(CookieName)
	if err != nil || cookie.Value == "" {
		return nil, errors.New("auth: no session cookie")
	}
	return a.resolveSession(r.Context(), cookie.Value)
}

// SessionByToken resolves a session token to its user without an HTTP
// request — the transport-free counterpart of Session for frontends
// that persist the cookie value and never see a request (cmd/gui's
// "remember me" file). Expiry, revocation and pruning behave exactly as
// in Session: one validation path, two entry points.
func (a *Auth) SessionByToken(ctx context.Context, token string) (*ValidatedSession, error) {
	if token == "" {
		return nil, errors.New("auth: no session token")
	}
	return a.resolveSession(ctx, token)
}

// resolveSession is the shared validation: token hash lookup, expiry
// check, owner load.
func (a *Auth) resolveSession(ctx context.Context, token string) (*ValidatedSession, error) {
	row, err := a.q.GetSession(ctx, tokenHash(token))
	if err != nil {
		return nil, errors.New("auth: unknown session")
	}
	expires, err := time.Parse(db.TimeFormat, row.Expires)
	if err != nil {
		return nil, fmt.Errorf("auth: session expiry unreadable: %w", err)
	}
	if time.Now().UTC().After(expires) {
		_ = a.q.DeleteSession(ctx, row.TokenHash)
		secEvent(ctx, evSessionExpired, row.UserID)
		return nil, errors.New("auth: session expired")
	}
	user, err := a.q.GetUser(ctx, row.UserID)
	if err != nil {
		secEvent(ctx, evSessionOrphan, row.UserID)
		return nil, errors.New("auth: session owner gone")
	}
	return &ValidatedSession{User: &User{ID: user.ID, Email: user.Email}, Expires: expires}, nil
}

// sessionCookie builds the cookie for a fresh login. Exported as
// SetSessionCookie so non-API logins (the /login form) issue the exact
// same cookie instead of duplicating its attributes.
func (a *Auth) sessionCookie(token string) *http.Cookie {
	return a.SetSessionCookie(token)
}

// SetSessionCookie builds the login cookie. See sessionCookie.
func (a *Auth) SetSessionCookie(token string) *http.Cookie {
	// #nosec G124 — Secure is false in dev (plain-HTTP localhost) by explicit
	// config; production boots with secureCookies and the real flag.
	return &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// clearCookie expires the session cookie (logout + invalid-token cleanup).
func (a *Auth) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, a.ClearSessionCookie())
}

// ClearSessionCookie builds the cookie-expiring counterpart to
// SetSessionCookie. Exported for the /logout form handler.
func (a *Auth) ClearSessionCookie() *http.Cookie {
	// #nosec G124 — same deliberate dev/prod split as sessionCookie above.
	return &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// ctxKey carries the validated session. Private so only SessionFrom reads it.
type ctxKey struct{}

// RequireSession is chi middleware: requests without a valid session get
// 401 with the cookie cleared, the rest reach the handler with the session
// in context. Data endpoints that prefer a login redirect (303) resolve
// the session themselves via Session instead.
func (a *Auth) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := a.Session(r)
		if err != nil {
			a.clearCookie(w)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, sess)))
	})
}

// SessionFrom returns the session RequireSession stored in the context.
func SessionFrom(ctx context.Context) (*ValidatedSession, error) {
	sess, ok := ctx.Value(ctxKey{}).(*ValidatedSession)
	if !ok || sess == nil {
		return nil, errors.New("auth: no session in context")
	}
	return sess, nil
}
