// SCOPE:feature — HTTP todo pages (login/logout/todo index)
//
// Page endpoints: same paths, redirects and rendered components (the shared
// LoginPage and Layout/TodoPage templates — no forked markup), with data
// from dbgen and internal/auth.
package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/calionauta/gogogo/features/authviews"
	"github.com/calionauta/gogogo/features/todo/components"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/demo"
)

// loginNext resolves the post-login destination, defaulting to the demo
// app. Only same-origin paths are honored: anything else falls back to
// /todo, so no ?next= value can become an open redirect.
func loginNext(r *http.Request) string {
	next := r.FormValue("next")
	if next == "" {
		next = r.URL.Query().Get("next")
	}
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "/todo"
	}
	return next
}

// loginNotice explains an automatic bounce from the auth guard
// (?auth=required, set by internal/rbac), so the login page does not look like
// a dead end the user reached by mistake.
func loginNotice(r *http.Request) string {
	if r.URL.Query().Get("auth") != "required" {
		return ""
	}
	return "You need to sign in to open that page."
}

func (h *Handler) handleLoginGet(w http.ResponseWriter, r *http.Request) {
	if h.Auth.OpenMode() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	//nolint:contextcheck // templ components receive ctx from Render; the generated boundary is invisible to the lint.
	_ = authviews.LoginPage(loginNext(r), "", h.DemoAccounts, "", loginNotice(r), h.SignupEnabled).Render(r.Context(), w)
}

func (h *Handler) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	if h.Auth.OpenMode() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	ctx := auth.WithUserAgent(r.Context(), r.UserAgent())
	r = r.WithContext(ctx)
	if err := r.ParseForm(); err != nil {
		h.renderLogin(w, r, "Invalid form submission", "", "")
		return
	}
	switch r.FormValue("intent") {
	case "totp":
		h.handleTOTPCode(w, r)
	case "magic":
		h.handleMagicLinkForm(w, r)
	case "forgot":
		h.handleForgotForm(w, r)
	case "verify":
		h.handleVerifyForm(w, r)
	default:
		h.handlePasswordLogin(w, r)
	}
}

func (h *Handler) renderLogin(w http.ResponseWriter, r *http.Request, errMsg, notice, totpUser string) {
	if notice == "" {
		notice = loginNotice(r)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	//nolint:contextcheck // templ components receive ctx from Render.
	_ = authviews.LoginPage(loginNext(r), errMsg, h.DemoAccounts, totpUser, notice, h.SignupEnabled).
		Render(r.Context(), w)
}

// signupAvailable is the one gate GET and POST share: open auth has no
// account to create, and a deployment with signup closed must not reveal the
// page at all.
func (h *Handler) signupAvailable(w http.ResponseWriter, r *http.Request) bool {
	if h.Auth.OpenMode() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return false
	}
	if !h.SignupEnabled {
		http.NotFound(w, r)
		return false
	}
	return true
}

func (h *Handler) handleSignupGet(w http.ResponseWriter, r *http.Request) {
	if !h.signupAvailable(w, r) {
		return
	}
	// Re-entry: a signed-in visitor goes to the app, not a second account.
	if sess, err := h.Auth.Session(r); err == nil && sess != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	h.renderSignup(w, r, "", "")
}

func (h *Handler) handleSignupPost(w http.ResponseWriter, r *http.Request) {
	if !h.signupAvailable(w, r) {
		return
	}
	ctx := auth.WithUserAgent(r.Context(), r.UserAgent())
	r = r.WithContext(ctx)
	if err := r.ParseForm(); err != nil {
		h.renderSignup(w, r, "Invalid form submission.", "")
		return
	}
	if r.PostFormValue("password") != r.PostFormValue("confirm") {
		h.renderSignup(w, r, "Passwords do not match.", "")
		return
	}
	user, token, err := h.Auth.Signup(
		ctx,
		r.PostFormValue("email"),
		r.PostFormValue("password"),
		strings.TrimSpace(r.PostFormValue("name")),
	)
	if err != nil {
		h.renderSignup(w, r, signupMessage(err), "")
		return
	}
	if h.Auth.RequiresEmailVerification() {
		// The signup session is a credential too: revoke it, then send the
		// link. The verify endpoint needs no session.
		_ = h.Auth.RevokeAllSessions(ctx, user.ID)
		if verr := h.Auth.RequestEmailVerificationDefault(ctx, user.Email); verr != nil {
			h.renderSignup(w, r, "Email verification is not configured on this deployment.", "")
			return
		}
		h.renderSignup(w, r, "", "Check your email to verify the address, then sign in.")
		return
	}
	http.SetCookie(w, h.Auth.SetSessionCookie(token))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) renderSignup(w http.ResponseWriter, r *http.Request, errMsg, notice string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	//nolint:contextcheck // templ components receive ctx from Render.
	_ = authviews.SignupPage(loginNext(r), errMsg, notice).Render(r.Context(), w)
}

// signupMessage turns a precise Signup failure into the sentence the form
// shows. The codes are internal/auth's contract; the words are product copy.
func signupMessage(err error) string {
	switch {
	case errors.Is(err, auth.ErrPasswordTooShort):
		return "Use at least 8 characters for the password."
	case errors.Is(err, auth.ErrPasswordTooLong):
		return "That password is too long."
	case errors.Is(err, auth.ErrInvalidEmail):
		return "Enter a valid email address."
	case errors.Is(err, auth.ErrEmailTaken):
		return "That email is already registered. Sign in instead."
	default:
		return "Could not create the account. Try again."
	}
}

func (h *Handler) handlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	token, totpRequired, err := h.authLogin(r)
	if err != nil {
		if errors.Is(err, auth.ErrEmailUnverified) {
			h.renderLogin(w, r, "Email not verified — use the resend option below.", "", "")
			return
		}
		h.renderLogin(w, r, "Wrong email or password", "", "")
		return
	}
	if totpRequired {
		email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
		row, qerr := h.Q.GetUserByEmail(r.Context(), email)
		if qerr != nil {
			h.renderLogin(w, r, "Two-factor verification failed", "", "")
			return
		}
		h.renderLogin(w, r, "", "Enter the 6-digit code from your authenticator.", row.ID)
		return
	}
	http.SetCookie(w, h.Auth.SetSessionCookie(token))
	// #nosec G710 — loginNext allows same-origin paths only (/todo fallback).
	http.Redirect(w, r, loginNext(r), http.StatusSeeOther)
}

func (h *Handler) handleTOTPCode(w http.ResponseWriter, r *http.Request) {
	uid := r.FormValue("totp_user")
	token, err := h.Auth.VerifyTOTPLogin(r.Context(), uid, strings.TrimSpace(r.FormValue("totp_code")))
	if err != nil {
		h.renderLogin(w, r, "Wrong or expired code.", "", uid)
		return
	}
	http.SetCookie(w, h.Auth.SetSessionCookie(token))
	// #nosec G710 — same-origin path validation above.
	http.Redirect(w, r, loginNext(r), http.StatusSeeOther)
}

func (h *Handler) handleMagicLinkForm(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" {
		h.renderLogin(w, r, "Enter your email address.", "", "")
		return
	}
	if err := h.Auth.RequestMagicLinkDefault(r.Context(), email); err != nil {
		h.renderLogin(w, r, "Email sign-in is not configured on this deployment.", "", "")
		return
	}
	h.renderLogin(w, r, "", "If that address has an account, a sign-in link is on its way.", "")
}

func (h *Handler) handleForgotForm(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" {
		h.renderLogin(w, r, "Enter your email address.", "", "")
		return
	}
	if err := h.Auth.RequestPasswordResetDefault(r.Context(), email); err != nil {
		h.renderLogin(w, r, "Password reset is not configured on this deployment.", "", "")
		return
	}
	h.renderLogin(w, r, "", "If that address has an account, a reset link is on its way.", "")
}

func (h *Handler) handleVerifyForm(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" {
		h.renderLogin(w, r, "Enter your email address.", "", "")
		return
	}
	if err := h.Auth.RequestEmailVerificationDefault(r.Context(), email); err != nil {
		h.renderLogin(w, r, "Email verification is not configured on this deployment.", "", "")
		return
	}
	h.renderLogin(w, r, "", "If that address needs verification, a link is on its way.", "")
}

// authLogin validates form credentials, keeping the handler body to the
// same shape as the other mutation entries (resolve → act → respond).
func (h *Handler) authLogin(r *http.Request) (string, bool, error) {
	return h.Auth.Login(r.Context(), r.FormValue("email"), r.FormValue("password"))
}

func (h *Handler) handleLogoutPost(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.CookieName); err == nil {
		h.Auth.Logout(r.Context(), cookie.Value)
	}
	http.SetCookie(w, h.Auth.ClearSessionCookie())
	// Sign-out is a shared-device boundary: drop this origin's caches,
	// IndexedDB (the outbox) and the service-worker registration in ONE
	// response header, so nothing queued by this account can replay under the
	// next one. The browser applies it to a redirect like any other response.
	w.Header().Set("Clear-Site-Data", `"cache", "storage"`)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// handleIndex serves the demo Todo page on the Layout/TodoPage
// components, with guests bounced to /login and data from dbgen.
func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	owner, sess, err := h.ownerSession(r)
	if err != nil {
		redirectLogin(w, r)
		return
	}
	todos, err := h.listVisible(r.Context(), owner, sess.Role, "all", "all")
	if err != nil {
		// Not a fake empty list: an unrunnable query would render as "you
		// have no todos", which is indistinguishable from the real thing and
		// tells the operator nothing. Fail loudly instead — the log carries
		// the cause.
		h.fail(r.Context(), w, "todo: index list failed", err, "error listing todos")
		return
	}
	// The page load is the one render where the URL's ?q= is authoritative: it
	// seeds the box before the client owns $searchQuery (see displaySignals).
	signals := h.displaySignalsWithQuery(r, todos, "all", "all", searchQueryOf(r))
	signals.ConnectedClients = h.Hub.CountUserClients()
	signals.Suggestions = []string{}
	signals.SuggestErr = ""
	signals.RealtimeKind = h.realtimeKind()
	signals.StoreLabel = "Turso Database"
	signals.OfflineLabel = h.offlineLabel()
	signals.SidebarTab = "queue"
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = components.Layout(
		"Todos — gogogo",
		signals, sess.User.Email, sess.Role,
		h.BuildLabel, h.BuildCommit,
		h.OfflineSyncEnabled,
		demo.On(r),
	).Render(r.Context(), w)
}
