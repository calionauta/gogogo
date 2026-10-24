// SCOPE:core — Auth plugin HTTP routes (magic-link, TOTP, OAuth)
package auth

import (
	"crypto/subtle"
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

// magicBindCookie binds the emailed link to the browser that requested it.
// Path-scoped so it never travels with ordinary application requests.
const magicBindCookie = "gogogo_magic_bind"

func (a *Auth) handleMagicRequest(w http.ResponseWriter, r *http.Request) {
	base := a.publicBaseURL()
	if base == "" {
		// Fail closed: building a link from r.Host lets a forged Host
		// redirect the token to an attacker (and behind a TLS proxy the
		// scheme would be http). APP_URL is the only trusted origin.
		http.Error(w, "magic link unavailable", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	if err := readJSON(r, &body); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	sender := a.magicSender()
	if sender == nil {
		// Uniform answer for every address: a 500 only for known accounts
		// would turn this endpoint into an existence oracle.
		http.Error(w, "magic link unavailable", http.StatusServiceUnavailable)
		return
	}
	if !a.magicAttempts.allow(signinIP(r)) {
		a.secReject(r.Context(), evMagicRejected, reasonThrottled, "", "")
		http.Error(w, "too many attempts", http.StatusTooManyRequests)
		return
	}
	var bind string
	if a.magicBindEnabled() {
		var err error
		bind, _, err = newToken()
		if err != nil {
			http.Error(w, "request failed", http.StatusInternalServerError)
			return
		}
		// #nosec G124 — Secure follows the environment; HttpOnly and SameSite set.
		http.SetCookie(w, &http.Cookie{
			Name:     magicBindCookie,
			Value:    bind,
			Path:     "/api/auth/magic",
			MaxAge:   int(magicLinkTTL.Seconds()),
			HttpOnly: true,
			Secure:   a.secure,
			SameSite: http.SameSiteLaxMode,
		})
	}
	// Always 200: success and unknown addresses are indistinguishable.
	if err := a.RequestMagicLink(r.Context(), body.Email, base, bind, sender); err != nil {
		http.Error(w, "request failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{jsonStatusKey: "ok"})
}

// handleMagicVerify is the emailed GET. It is deliberately read-only: it
// resolves the account, checks the optional browser binding, and renders a
// confirmation form. The POST consumes the token. This keeps email scanners
// (which prefetch links) from spending it and makes cross-device links work.
func (a *Auth) handleMagicVerify(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	email, ok := a.LookupMagicToken(r.Context(), token)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	bind := r.URL.Query().Get("bind")
	if a.magicBindEnabled() {
		cookie, cerr := r.Cookie(magicBindCookie)
		if bind == "" || cerr != nil ||
			subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(bind)) != 1 {
			a.secReject(r.Context(), evMagicRejected, reasonBrowserMismatch, "", "")
			a.renderMagicConfirm(w, r, email, token, bind,
				"Open this link in the browser where you requested it, or request a new link.")
			return
		}
	} else {
		bind = ""
	}
	a.renderMagicConfirm(w, r, email, token, bind, "")
}

// handleMagicComplete consumes the token after the user submits the
// confirmation page. The unsafe method is covered by SameSite + Origin +
// Sec-Fetch-Site at the edge.
func (a *Auth) handleMagicComplete(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(nil, r.Body, maxAuthBody)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	token := r.PostFormValue("token")
	bind := r.PostFormValue("bind")
	email, ok := a.LookupMagicToken(r.Context(), token)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if a.magicBindEnabled() {
		cookie, cerr := r.Cookie(magicBindCookie)
		if bind == "" || cerr != nil ||
			subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(bind)) != 1 {
			a.secReject(r.Context(), evMagicRejected, reasonBrowserMismatch, "", "")
			a.renderMagicConfirm(w, r, email, token, bind,
				"Open this link in the browser where you requested it, or request a new link.")
			return
		}
		// #nosec G124 — clearing cookie; Secure follows the environment.
		http.SetCookie(w, &http.Cookie{
			Name: magicBindCookie, Value: "", Path: "/api/auth/magic", MaxAge: -1,
			HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode,
		})
	}
	session, err := a.VerifyMagicLink(r.Context(), token)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, a.sessionCookie(session))
	http.Redirect(w, r, "/todo", http.StatusSeeOther)
}

// renderMagicConfirm renders the read-only landing. No renderer is a boot
// wiring error: fail closed instead of silently completing from a GET.
func (a *Auth) renderMagicConfirm(w http.ResponseWriter, r *http.Request, email, token, bind, errMsg string) {
	if page := a.magicConfirmPage(); page != nil {
		page(w, r, email, token, bind, errMsg)
		return
	}
	http.Error(w, "magic link confirmation unavailable", http.StatusInternalServerError)
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
	// Revoke before lowering the factor: a failure leaves the account
	// logged out with TOTP still on (safe) instead of weakening auth while
	// older sessions survive.
	if rerr := a.RevokeAllSessions(r.Context(), user.ID); rerr != nil {
		http.Error(w, "disable failed", http.StatusInternalServerError)
		return
	}
	if derr := a.DisableTOTP(r.Context(), user.ID); derr != nil {
		http.Error(w, "disable failed", http.StatusInternalServerError)
		return
	}
	token, err := a.openSession(r.Context(), user.ID)
	if err != nil {
		http.Error(w, "disable failed", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, a.sessionCookie(token))
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
	hasPassword, herr := a.HasPassword(r.Context(), user.ID)
	if herr != nil {
		http.Error(w, "issue failed", http.StatusInternalServerError)
		return
	}
	if hasPassword {
		var body struct {
			Password string `json:"password"`
		}
		if berr := readJSON(r, &body); berr != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if !a.VerifyUserPassword(r.Context(), user.ID, body.Password) {
			a.RecordAudit(r.Context(), EventStepUpRejected,
				"recovery codes", user.Email, user.ID, "")
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
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
