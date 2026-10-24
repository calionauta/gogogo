// SCOPE:feature — Authenticated account security page.
//
// One vertical slice for the things a signed-in user owns: email
// verification, password, TOTP enrollment/recovery, and session revocation.
// It reuses internal/auth operations; no new policy engine or service layer.
package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/account/components"
	"github.com/calionauta/gogogo/internal/auth"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// Handler serves /account.
type Handler struct {
	cfg *config.Config
	a   *auth.Auth
	q   *dbgen.Queries
}

// NewHandler builds the account slice.
func NewHandler(cfg *config.Config, a *auth.Auth, q *dbgen.Queries) *Handler {
	if cfg == nil || a == nil || q == nil {
		panic("account: NewHandler with nil dependency")
	}
	return &Handler{cfg: cfg, a: a, q: q}
}

// Routes returns the account router.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/account", h.index)
	r.Post("/account/password", h.changePassword)
	r.Post("/account/verify", h.resendVerification)
	r.Post("/account/totp/begin", h.beginTOTP)
	r.Post("/account/totp/verify", h.verifyTOTP)
	r.Post("/account/totp/disable", h.disableTOTP)
	r.Post("/account/totp/recovery", h.recoveryCodes)
	r.Post("/account/sessions/revoke", h.revokeSession)
	r.Post("/account/sessions/revoke-all", h.revokeAll)
	return r
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(w, r)
	if !ok {
		return
	}
	data, err := h.pageData(r, sess)
	if err != nil {
		http.Error(w, "account unavailable", http.StatusInternalServerError)
		return
	}
	data.Notice = firstNonEmpty(r.URL.Query().Get("notice"), r.URL.Query().Get("reset"))
	h.render(w, r, data)
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	ctx := auth.WithUserAgent(r.Context(), r.UserAgent())
	sess, ok := h.session(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, sess, "Invalid form submission.")
		return
	}
	if r.PostFormValue("new") != r.PostFormValue("confirm") {
		h.renderError(w, r, sess, "New passwords do not match.")
		return
	}
	token, err := h.a.ChangePassword(ctx, sess.User.ID,
		r.PostFormValue("current"), r.PostFormValue("new"))
	if err != nil {
		if errors.Is(err, auth.ErrBadCredentials) {
			h.renderError(w, r, sess, "Current password is wrong.")
			return
		}
		if errors.Is(err, auth.ErrPasswordTooLong) {
			h.renderError(w, r, sess, "Choose a valid new password (min 8 characters).")
			return
		}
		h.renderError(w, r, sess, "Password change failed.")
		return
	}
	http.SetCookie(w, h.a.SetSessionCookie(token))
	http.Redirect(w, r, "/account?notice=Password+changed", http.StatusSeeOther)
}

func (h *Handler) resendVerification(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(w, r)
	if !ok {
		return
	}
	if err := h.a.RequestEmailVerificationDefault(r.Context(), sess.User.Email); err != nil {
		h.renderError(w, r, sess, "Email verification is not configured on this deployment.")
		return
	}
	http.Redirect(w, r, "/account?notice=Verification+link+sent", http.StatusSeeOther)
}

func (h *Handler) beginTOTP(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(w, r)
	if !ok {
		return
	}
	secret, url, _, err := h.a.BeginTOTP(r.Context(), sess.User.ID)
	if err != nil {
		h.renderError(w, r, sess, "TOTP is not configured (set AUTH_ENC_KEY).")
		return
	}
	data, derr := h.pageData(r, sess)
	if derr != nil {
		http.Error(w, "account unavailable", http.StatusInternalServerError)
		return
	}
	data.SetupSecret = secret
	data.SetupURL = url
	h.render(w, r, data)
}

func (h *Handler) verifyTOTP(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, sess, "Invalid form submission.")
		return
	}
	if err := h.a.VerifyTOTPEnroll(r.Context(), sess.User.ID, strings.TrimSpace(r.PostFormValue("code"))); err != nil {
		data, derr := h.pageData(r, sess)
		if derr != nil {
			http.Error(w, "account unavailable", http.StatusInternalServerError)
			return
		}
		data.Error = "Wrong or expired code."
		h.render(w, r, data)
		return
	}
	http.Redirect(w, r, "/account?notice=Two-factor+enabled", http.StatusSeeOther)
}

func (h *Handler) disableTOTP(w http.ResponseWriter, r *http.Request) {
	ctx := auth.WithUserAgent(r.Context(), r.UserAgent())
	sess, ok := h.session(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, sess, "Invalid form submission.")
		return
	}
	hasPassword, perr := h.a.HasPassword(ctx, sess.User.ID)
	if perr != nil {
		h.renderError(w, r, sess, "Could not verify this account. Try again.")
		return
	}
	var token string
	var err error
	if hasPassword {
		token, err = h.a.DisableTOTPWithPassword(ctx, sess.User.ID, r.PostFormValue("password"))
	} else {
		token, err = h.a.DisableTOTPWithCode(ctx, sess.User.ID, r.PostFormValue("code"))
	}
	if err != nil {
		if errors.Is(err, auth.ErrBadCredentials) {
			h.renderError(w, r, sess, "The proof is wrong.")
			return
		}
		h.renderError(w, r, sess, "Could not disable two-factor.")
		return
	}
	http.SetCookie(w, h.a.SetSessionCookie(token))
	http.Redirect(w, r, "/account?notice=Two-factor+disabled", http.StatusSeeOther)
}

func (h *Handler) recoveryCodes(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, sess, "Invalid form submission.")
		return
	}
	hasPassword, perr := h.a.HasPassword(r.Context(), sess.User.ID)
	if perr != nil {
		h.renderError(w, r, sess, "Could not verify this account. Try again.")
		return
	}
	var codes []string
	var err error
	if hasPassword {
		if !h.a.VerifyUserPassword(r.Context(), sess.User.ID, r.PostFormValue("password")) {
			h.a.RecordAudit(r.Context(), auth.EventStepUpRejected,
				"recovery codes", sess.User.Email, sess.User.ID, sess.SessionID)
			h.renderError(w, r, sess, "Password is wrong.")
			return
		}
		codes, err = h.a.RotateRecoveryCodes(r.Context(), sess.User.ID)
	} else {
		codes, err = h.a.RotateRecoveryCodesWithCode(r.Context(), sess.User.ID, r.PostFormValue("code"))
		if errors.Is(err, auth.ErrBadCredentials) {
			h.a.RecordAudit(r.Context(), auth.EventStepUpRejected,
				"recovery codes", sess.User.Email, sess.User.ID, sess.SessionID)
			h.renderError(w, r, sess, "The authenticator or recovery code is wrong.")
			return
		}
	}
	if err != nil {
		h.renderError(w, r, sess, "Could not generate recovery codes.")
		return
	}
	data, derr := h.pageData(r, sess)
	if derr != nil {
		http.Error(w, "account unavailable", http.StatusInternalServerError)
		return
	}
	data.RecoveryCodes = codes
	h.render(w, r, data)
}

func (h *Handler) revokeSession(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, sess, "Invalid form submission.")
		return
	}
	if err := h.a.RevokeSessionByID(r.Context(), sess.User.ID, r.PostFormValue("session_id")); err != nil {
		h.renderError(w, r, sess, "Could not revoke that session.")
		return
	}
	http.Redirect(w, r, "/account?notice=Session+revoked", http.StatusSeeOther)
}

func (h *Handler) revokeAll(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, sess, "Invalid form submission.")
		return
	}
	hasPassword, perr := h.a.HasPassword(r.Context(), sess.User.ID)
	if perr != nil {
		h.renderError(w, r, sess, "Could not verify this account. Try again.")
		return
	}
	if hasPassword &&
		!h.a.VerifyUserPassword(r.Context(), sess.User.ID, r.PostFormValue("password")) {
		h.a.RecordAudit(r.Context(), auth.EventStepUpRejected,
			"revoke all", sess.User.Email, sess.User.ID, sess.SessionID)
		h.renderError(w, r, sess, "Password is wrong.")
		return
	}
	if err := h.a.RevokeAllSessions(r.Context(), sess.User.ID); err != nil {
		h.renderError(w, r, sess, "Could not sign out every session.")
		return
	}
	http.SetCookie(w, h.a.ClearSessionCookie())
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handler) session(w http.ResponseWriter, r *http.Request) (*auth.ValidatedSession, bool) {
	sess, err := h.a.Session(r)
	if err != nil || sess == nil || sess.User == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return nil, false
	}
	return sess, true
}

func (h *Handler) pageData(r *http.Request, sess *auth.ValidatedSession) (components.PageData, error) {
	email, verified, totp, err := h.a.UserByID(r.Context(), sess.User.ID)
	if err != nil {
		return components.PageData{}, err
	}
	rows, err := h.q.ListSessionsByUser(r.Context(), sess.User.ID)
	if err != nil {
		return components.PageData{}, err
	}
	views := make([]components.SessionView, 0, len(rows))
	for _, row := range rows {
		views = append(views, components.SessionView{
			ID: row.SessionID, ShortID: shortID(row.SessionID),
			Agent: deviceLabel(row.UserAgent), Created: formatWhen(row.Created),
			LastSeen: formatWhen(row.LastSeen), Expires: formatWhen(row.Expires),
			Current: row.SessionID == sess.SessionID,
		})
	}
	passwordSet, perr := h.a.HasPassword(r.Context(), sess.User.ID)
	if perr != nil {
		return components.PageData{}, perr
	}
	return components.PageData{
		Email: email, Role: sess.Role, Verified: verified, TOTP: totp,
		Sessions: views, PasswordSet: passwordSet,
	}, nil
}

// shortID keeps the row scannable while the full opaque id stays in title.
const sessionIDPrefixLen = 8

func shortID(id string) string {
	if len(id) <= sessionIDPrefixLen {
		return id
	}
	return id[:sessionIDPrefixLen] + "…"
}

// deviceLabel turns the stored user-agent into a short hint. It deliberately
// stays heuristic and dependency-free: the raw string is evidence, not a
// security claim, and an unknown client must still render something useful.
func deviceLabel(ua string) string {
	if ua == "" {
		return "Unknown device"
	}
	lower := strings.ToLower(ua)
	browser := browserName(lower)
	if osName := osLabel(lower); osName != "" {
		return browser + " on " + osName
	}
	return browser
}

func browserName(lower string) string {
	switch {
	case strings.Contains(lower, "edg/"):
		return "Edge"
	case strings.Contains(lower, "firefox/"):
		return "Firefox"
	case strings.Contains(lower, "chrome/") || strings.Contains(lower, "crios/"):
		return "Chrome"
	case strings.Contains(lower, "safari/"):
		return "Safari"
	default:
		return "Browser"
	}
}

func osLabel(lower string) string {
	switch {
	case strings.Contains(lower, "android"):
		return "Android"
	case strings.Contains(lower, "iphone") || strings.Contains(lower, "ipad") || strings.Contains(lower, "ios"):
		return "iOS"
	case strings.Contains(lower, "windows"):
		return "Windows"
	case strings.Contains(lower, "mac os") || strings.Contains(lower, "macintosh"):
		return "macOS"
	case strings.Contains(lower, "linux"):
		return "Linux"
	default:
		return ""
	}
}

// formatWhen renders a stored timestamp for a human. The audit table keeps the
// raw ISO string; the account page is about reading "when", not precision.
func formatWhen(raw string) string {
	if raw == "" {
		return ""
	}
	t, err := time.Parse(dbschema.TimeFormat, raw)
	if err != nil {
		return raw
	}
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

func (h *Handler) renderError(w http.ResponseWriter, r *http.Request, sess *auth.ValidatedSession, msg string) {
	data, err := h.pageData(r, sess)
	if err != nil {
		http.Error(w, "account unavailable", http.StatusInternalServerError)
		return
	}
	data.Error = msg
	h.render(w, r, data)
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, data components.PageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = components.Page(data, h.cfg.BuildLabel, h.cfg.BuildCommit).Render(r.Context(), w)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
