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

func (h *Handler) handleLoginGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	//nolint:contextcheck // templ components receive ctx from Render; the generated boundary is invisible to the lint.
	_ = authviews.LoginPage(loginNext(r), "", h.skinName(), h.DemoAccounts, "", "").Render(r.Context(), w)
}

func (h *Handler) handleLoginPost(w http.ResponseWriter, r *http.Request) {
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	//nolint:contextcheck // templ components receive ctx from Render.
	_ = authviews.LoginPage(loginNext(r), errMsg, h.skinName(), h.DemoAccounts, totpUser, notice).
		Render(r.Context(), w)
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
	skin := h.skinName()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = components.Layout(
		"Todos — gogogo",
		signals, sess.User.Email, sess.Role,
		h.BuildLabel, h.BuildCommit,
		h.OfflineSyncEnabled,
		skin,
		demo.On(r),
	).Render(r.Context(), w)
}
