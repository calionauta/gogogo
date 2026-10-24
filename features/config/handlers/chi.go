// SCOPE:feature — HTTP config view (admin-only, secrets masked)
//
// Same path, same component (shared Page over BuildPageData): the route is
// fenced to admins by rbac.RequireAdmin, independent of RBAC_ENABLED. Nothing
// secret-shaped ever renders raw — the masking lives in the components
// package, shared with the previous stack, so both pages redact identically.
package handlers

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/config/components"
	"github.com/calionauta/gogogo/internal/auth"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/rbac"
)

// Handler serves the config view on chi.
type Handler struct {
	cfg     *config.Config
	auth    *auth.Auth
	queries *dbgen.Queries
}

// NewHandler builds the chi config handler. Nil deps panic: wire errors
// surface at boot, not per request. queries serves the role lookup for
// the RBAC banner only (display, never a gate — enforcement stays in
// the middleware).
func NewHandler(cfg *config.Config, a *auth.Auth, q *dbgen.Queries) *Handler {
	if cfg == nil || a == nil || q == nil {
		panic("config chi: NewHandler with nil dependency")
	}
	return &Handler{cfg: cfg, auth: a, queries: q}
}

// Routes returns the chi router with the config view.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	// Operator surface: always admin, even when RBAC_ENABLED=false. The
	// policy keeps the same rule; this fence is what makes it true on the
	// template's default posture.
	gate := rbac.RequireAdmin(h.auth)
	r.With(gate).Get("/config", h.handleIndex)
	r.With(gate).Post("/config/users/role", h.handleRole)
	r.With(gate).Post("/config/users/revoke", h.handleRevokeSessions)
	return r
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	sess, err := h.auth.Session(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	email := ""
	role := ""
	if sess.User != nil {
		email = sess.User.Email
		role = sess.Role
	}
	data := components.BuildPageData(h.cfg)
	// Role lookup is display-only here: a miss renders "unknown". The
	// route gate is rbac.RequireAdmin above (always on), with the policy
	// as the second layer.
	if email != "" {
		if u, err := h.queries.GetUserByEmail(r.Context(), email); err == nil {
			data.Role = u.Role
		}
	}
	rows, uerr := h.queries.ListUsers(r.Context())
	if uerr != nil {
		http.Error(w, "users unavailable", http.StatusInternalServerError)
		return
	}
	data.Users = make([]components.UserRow, 0, len(rows))
	for _, row := range rows {
		data.Users = append(data.Users, components.UserRow{
			ID: row.ID, Email: row.Email, Role: row.Role, Created: row.Created,
			Verified: row.EmailVerifiedAt != "", TOTP: row.TotpVerified != 0,
		})
	}
	data.Notice = r.URL.Query().Get("notice")
	data.Error = r.URL.Query().Get("error")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = components.Page(
		data, email, role, h.cfg.BuildLabel, h.cfg.BuildCommit,
	).Render(r.Context(), w)
}

func (h *Handler) handleRole(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectConfig(w, r, "error", "Invalid form submission.")
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
	role := strings.TrimSpace(r.PostFormValue("role"))
	if !validRole(role) {
		redirectConfig(w, r, "error", "Unknown role.")
		return
	}
	target, err := h.queries.GetUserByEmail(r.Context(), email)
	if err != nil {
		redirectConfig(w, r, "error", "User not found.")
		return
	}
	if isAdminRole(target.Role) && !isAdminRole(role) {
		admins, cerr := h.queries.CountAdmins(r.Context())
		if cerr != nil {
			redirectConfig(w, r, "error", "Could not count admins.")
			return
		}
		if admins <= 1 {
			redirectConfig(w, r, "error", "Refusing to remove the last admin.")
			return
		}
	}
	if err := h.queries.SetUserRole(r.Context(), dbgen.SetUserRoleParams{
		Role: role, Updated: dbschema.Now(), Email: email,
	}); err != nil {
		redirectConfig(w, r, "error", "Could not change role.")
		return
	}
	redirectConfig(w, r, "notice", "Role updated.")
}

func (h *Handler) handleRevokeSessions(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectConfig(w, r, "error", "Invalid form submission.")
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
	user, err := h.queries.GetUserByEmail(r.Context(), email)
	if err != nil {
		redirectConfig(w, r, "error", "User not found.")
		return
	}
	if err := h.queries.DeleteUserSessions(r.Context(), user.ID); err != nil {
		redirectConfig(w, r, "error", "Could not revoke sessions.")
		return
	}
	redirectConfig(w, r, "notice", "Sessions revoked.")
}

func redirectConfig(w http.ResponseWriter, r *http.Request, key, msg string) {
	http.Redirect(w, r, "/config?"+key+"="+url.QueryEscape(msg), http.StatusSeeOther)
}
