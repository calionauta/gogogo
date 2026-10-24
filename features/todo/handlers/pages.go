// SCOPE:feature — HTTP todo pages (login/logout/todo index)
//
// Page endpoints: same paths, redirects and rendered components (the shared
// LoginPage and Layout/TodoPage templates — no forked markup), with data
// from dbgen and internal/auth.
package handlers

import (
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
	_ = authviews.LoginPage(loginNext(r), "", h.skinName(), h.DemoAccounts).Render(r.Context(), w)
}

func (h *Handler) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		//nolint:contextcheck // same templ boundary as the GET handler.
		_ = authviews.LoginPage(loginNext(r), "Invalid form submission", h.skinName(), h.DemoAccounts).Render(r.Context(), w)
		return
	}
	token, totpRequired, err := h.authLogin(r)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		//nolint:contextcheck // same templ boundary as the GET handler.
		_ = authviews.LoginPage(loginNext(r), "Wrong email or password", h.skinName(), h.DemoAccounts).Render(r.Context(), w)
		return
	}
	if totpRequired {
		// The shared form has no code field: 2FA accounts complete login
		// through the JSON API (/api/auth/totp/login). Say so plainly
		// instead of failing opaquely.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		//nolint:contextcheck // same templ boundary as the GET handler.
		_ = authviews.LoginPage(
			loginNext(r), "Two-factor required — finish login via an API client", h.skinName(), h.DemoAccounts,
		).Render(r.Context(), w)
		return
	}
	http.SetCookie(w, h.Auth.SetSessionCookie(token))
	// #nosec G710 — loginNext allows same-origin paths only (/todo fallback).
	http.Redirect(w, r, loginNext(r), http.StatusSeeOther)
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
