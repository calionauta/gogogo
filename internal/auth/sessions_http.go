// SCOPE:core — Session lifecycle routes: revoke-all and password change.
//
// Both revoke every other session before continuing. That is the point:
// a stolen cookie must stop working the moment the owner rotates a
// credential, and a "sign out everywhere" action has to include the
// device that clicked it (the new session cookie is the replacement).
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// handleRevokeAll signs the user out everywhere, including this request.
// The response clears the cookie; the caller may redirect to /login.
func (a *Auth) handleRevokeAll(w http.ResponseWriter, r *http.Request) {
	user, err := a.authedUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if rerr := a.RevokeAllSessions(r.Context(), user.ID); rerr != nil {
		http.Error(w, "revoke failed", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, a.ClearSessionCookie())
	writeJSON(w, map[string]string{jsonStatusKey: "ok"})
}

// RevokeAllSessions deletes every session row and every pending magic link
// for a user. The caller decides whether to issue a replacement session
// (password change) or clear the cookie (sign out everywhere).
func (a *Auth) RevokeAllSessions(ctx context.Context, userID string) error {
	if err := a.q.DeleteUserSessions(ctx, userID); err != nil {
		return fmt.Errorf("auth: revoke sessions: %w", err)
	}
	// A pending magic link is a login credential too: a password change or
	// "sign out everywhere" must invalidate it, not leave a 15-minute window.
	if err := a.q.DeleteMagicTokensForUser(ctx, userID); err != nil {
		return fmt.Errorf("auth: revoke magic links: %w", err)
	}
	return nil
}

// SetPassword hashes and stores a new password. Authorization is the
// caller's responsibility: the HTTP route verifies the current password
// first, and the demo seed uses it only to restore a disabled demo pair.
func (a *Auth) SetPassword(ctx context.Context, userID, password string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	if err := a.q.UpdateUserPassword(ctx, dbgen.UpdateUserPasswordParams{
		PasswordHash: hash, Updated: dbschema.Now(), ID: userID,
	}); err != nil {
		return fmt.Errorf("auth: set password: %w", err)
	}
	return nil
}

// handleListSessions returns the caller's active sessions. Session ids are
// opaque UUIDs; the token hash never leaves the server.
func (a *Auth) handleListSessions(w http.ResponseWriter, r *http.Request) {
	user, err := a.authedUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	rows, err := a.q.ListSessionsByUser(r.Context(), user.ID)
	if err != nil {
		http.Error(w, "list failed", http.StatusInternalServerError)
		return
	}
	type view struct {
		ID       string `json:"id"`
		Created  string `json:"created"`
		Expires  string `json:"expires"`
		LastSeen string `json:"lastSeen"`
	}
	out := make([]view, 0, len(rows))
	for _, row := range rows {
		out = append(out, view{ID: row.SessionID, Created: row.Created, Expires: row.Expires, LastSeen: row.LastSeen})
	}
	writeJSON(w, map[string]any{"sessions": out})
}

// handleRevokeSession revokes one session by its opaque id, scoped to the
// caller. Unknown ids are a no-op 200 (no oracle).
func (a *Auth) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	user, err := a.authedUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id := chi.URLParam(r, "id")
	if id != "" {
		if derr := a.q.DeleteSessionByID(r.Context(), dbgen.DeleteSessionByIDParams{
			UserID: user.ID, SessionID: id,
		}); derr != nil {
			http.Error(w, "revoke failed", http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]string{jsonStatusKey: "ok"})
}

// handlePasswordChange rotates the password, revokes every session, and
// opens a fresh one for this browser. The current password is proof the
// session owner is the account owner, not just a holder of the cookie.
func (a *Auth) handlePasswordChange(w http.ResponseWriter, r *http.Request) {
	user, err := a.authedUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct {
		Current string `json:"currentPassword"`
		New     string `json:"newPassword"`
	}
	if berr := readJSON(r, &body); berr != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	row, err := a.q.GetUser(r.Context(), user.ID)
	if err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if ok, _ := verifyPassword(row.PasswordHash, body.Current); !ok {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if verr := validatePassword(body.New); verr != nil {
		if errors.Is(verr, ErrPasswordTooLong) {
			http.Error(w, "password too long", http.StatusBadRequest)
			return
		}
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if rerr := a.RevokeAllSessions(r.Context(), user.ID); rerr != nil {
		http.Error(w, "change failed", http.StatusInternalServerError)
		return
	}
	if uerr := a.SetPassword(r.Context(), user.ID, body.New); uerr != nil {
		http.Error(w, "change failed", http.StatusInternalServerError)
		return
	}
	token, err := a.openSession(r.Context(), user.ID)
	if err != nil {
		http.Error(w, "change failed", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, a.sessionCookie(token))
	writeJSON(w, map[string]string{jsonStatusKey: "ok"})
}
