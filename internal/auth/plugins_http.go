// SCOPE:core — Auth plugin HTTP routes (magic-link, TOTP, OAuth)
package auth

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// SetMagicSender installs the magic-link delivery callback (email sender).
// Nil (the default) makes /api/auth/magic/request fail closed: passwordless
// login without a sender would issue links nobody receives.
func (a *Auth) SetMagicSender(send MagicSender) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sender = send
}

func (a *Auth) magicSender() MagicSender {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sender
}

// authedUser resolves the session user for API routes that need a login
// without the RequireSession middleware shape.
func (a *Auth) authedUser(r *http.Request) (*User, error) {
	sess, err := a.Session(r)
	if err != nil {
		return nil, err
	}
	return sess.User, nil
}

// baseURL rebuilds the public base from the request (scheme + host) for
// magic links. Behind TLS-terminating proxies the app should set the real
// external URL — document, don't guess headers here.
func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (a *Auth) handleMagicRequest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if err := readJSON(r, &body); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	// Always 200: success and unknown addresses are indistinguishable.
	if err := a.RequestMagicLink(r.Context(), body.Email, baseURL(r), a.magicSender()); err != nil {
		http.Error(w, "request failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{jsonStatusKey: "ok"})
}

func (a *Auth) handleMagicVerify(w http.ResponseWriter, r *http.Request) {
	token, err := a.VerifyMagicLink(r.Context(), r.URL.Query().Get("token"))
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, a.sessionCookie(token))
	http.Redirect(w, r, "/todo", http.StatusSeeOther)
}

func (a *Auth) handleTOTPBegin(w http.ResponseWriter, r *http.Request) {
	user, err := a.authedUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	secret, url, _, err := a.BeginTOTP(r.Context(), user.ID)
	if err != nil {
		http.Error(w, "enroll failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"secret": secret, "url": url})
}

func (a *Auth) handleTOTPVerify(w http.ResponseWriter, r *http.Request) {
	user, err := a.authedUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := readJSON(r, &body); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := a.VerifyTOTPEnroll(r.Context(), user.ID, strings.TrimSpace(body.Code)); err != nil {
		http.Error(w, "invalid code", http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]string{jsonStatusKey: "ok"})
}

func (a *Auth) handleTOTPLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID string `json:"userId"`
		Code   string `json:"code"`
	}
	if err := readJSON(r, &body); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	token, err := a.VerifyTOTPLogin(r.Context(), body.UserID, strings.TrimSpace(body.Code))
	if err != nil {
		http.Error(w, "invalid code", http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, a.sessionCookie(token))
	writeJSON(w, map[string]string{jsonStatusKey: "ok"})
}

func (a *Auth) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	user, err := a.authedUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if berr := readJSON(r, &body); berr != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	// Fresh password proof before removing the second factor: a stolen
	// session alone must not downgrade the account's protection.
	row, err := a.q.GetUser(r.Context(), user.ID)
	if err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if ok, _ := verifyPassword(row.PasswordHash, body.Password); !ok {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if err := a.DisableTOTP(r.Context(), user.ID); err != nil {
		http.Error(w, "disable failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{jsonStatusKey: "ok"})
}

// handleTOTPRecovery mints recovery codes for an authenticated user. The
// plaintext batch is returned exactly once — the caller displays it and
// the server never stores it.
func (a *Auth) handleTOTPRecovery(w http.ResponseWriter, r *http.Request) {
	user, err := a.authedUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	codes, err := a.RotateRecoveryCodes(r.Context(), user.ID)
	if err != nil {
		http.Error(w, "issue failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"codes": codes})
}

// handleOAuthStart begins /api/auth/oauth/{provider}. The provider comes from
// the router parameter, so an unknown shape never reaches the handler.
func (a *Auth) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	a.OAuthStart(w, r, chi.URLParam(r, "provider"))
}

// handleOAuthCallback finishes /api/auth/oauth/{provider}/callback.
func (a *Auth) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	a.OAuthCallback(w, r, chi.URLParam(r, "provider"))
}
