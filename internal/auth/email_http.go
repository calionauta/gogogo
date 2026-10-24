// SCOPE:core — Email verification and password-reset HTTP surface.
package auth

import (
	"errors"
	"net/http"
)

func (a *Auth) handleVerifyRequest(w http.ResponseWriter, r *http.Request) {
	if a.publicBaseURL() == "" || a.mailerFn() == nil {
		http.Error(w, "verification unavailable", http.StatusServiceUnavailable)
		return
	}
	if !a.verifyAttempts.allow(signinIP(r)) {
		http.Error(w, "too many attempts", http.StatusTooManyRequests)
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	if err := readJSON(r, &body); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	// Unknown and already-verified addresses are silent.
	_ = a.RequestEmailVerificationByEmail(r.Context(), body.Email, a.publicBaseURL())
	writeJSON(w, map[string]string{jsonStatusKey: "ok"})
}

func (a *Auth) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	if _, err := a.VerifyEmail(r.Context(), r.URL.Query().Get("token")); err != nil {
		http.Redirect(w, r, "/login?verified=0", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login?verified=1", http.StatusSeeOther)
}

func (a *Auth) handlePasswordResetRequest(w http.ResponseWriter, r *http.Request) {
	if a.publicBaseURL() == "" || a.mailerFn() == nil {
		http.Error(w, "reset unavailable", http.StatusServiceUnavailable)
		return
	}
	if !a.resetAttempts.allow(signinIP(r)) {
		http.Error(w, "too many attempts", http.StatusTooManyRequests)
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	if err := readJSON(r, &body); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	// Unknown/invalid addresses are silent, exactly like a sent link.
	_ = a.RequestPasswordReset(r.Context(), body.Email, a.publicBaseURL())
	writeJSON(w, map[string]string{jsonStatusKey: "ok"})
}

func (a *Auth) handlePasswordResetForm(w http.ResponseWriter, r *http.Request) {
	email, ok := a.LookupEmailToken(r.Context(), r.URL.Query().Get("token"), purposeReset)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	a.renderPasswordReset(w, r, email, r.URL.Query().Get("token"), "")
}

func (a *Auth) handlePasswordResetComplete(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(nil, r.Body, maxAuthBody)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	token := r.PostFormValue("token")
	email, ok := a.LookupEmailToken(r.Context(), token, purposeReset)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	password := r.PostFormValue("password")
	if password != r.PostFormValue("confirm") {
		a.renderPasswordReset(w, r, email, token, "Passwords do not match.")
		return
	}
	if verr := validatePassword(password); verr != nil {
		a.renderPasswordReset(w, r, email, token, "Choose a valid password (min 8 characters).")
		return
	}
	if err := a.ResetPassword(r.Context(), token, password); err != nil {
		if errors.Is(err, ErrPasswordTooLong) || errors.Is(err, ErrBadCredentials) {
			a.renderPasswordReset(w, r, email, token, "Choose a valid password (min 8 characters).")
			return
		}
		http.Error(w, "reset failed", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/login?reset=1", http.StatusSeeOther)
}

func (a *Auth) renderPasswordReset(w http.ResponseWriter, r *http.Request, email, token, errMsg string) {
	if page := a.passwordResetRenderer(); page != nil {
		page(w, r, email, token, errMsg)
		return
	}
	http.Error(w, "password reset page unavailable", http.StatusInternalServerError)
}
