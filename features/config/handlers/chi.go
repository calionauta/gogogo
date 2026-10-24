// SCOPE:feature — HTTP config view (admin-only, secrets masked)
//
// Same path, same component (shared Page over BuildPageData): the route is
// fenced to admins by rbac.RequireAdmin, independent of RBAC_ENABLED. Nothing
// secret-shaped ever renders raw — the masking lives in the components
// package, shared with the previous stack, so both pages redact identically.
package handlers

import (
	"encoding/csv"
	"net/http"
	"net/url"
	"strings"
	"time"

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
	r.With(gate).Get("/config/audit.csv", h.handleAuditCSV)
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
			ID: row.ID, Email: row.Email, Role: row.Role, Created: formatWhen(row.Created),
			Verified: row.EmailVerifiedAt != "", TOTP: row.TotpVerified != 0,
		})
	}
	events, eerr := h.queries.ListSecurityEvents(r.Context(), 100)
	if eerr != nil {
		http.Error(w, "audit unavailable", http.StatusInternalServerError)
		return
	}
	data.Events = make([]components.SecurityEventRow, 0, len(events))
	for _, event := range events {
		data.Events = append(data.Events, components.SecurityEventRow{
			Event: event.Event, Reason: event.Reason, Email: event.Email,
			UserID: event.UserID, ActorID: event.ActorID, Created: formatWhen(event.Created),
		})
	}
	counts, cerr := h.queries.SecurityEventCountsSince(r.Context(),
		time.Now().UTC().Add(-24*time.Hour).Format(dbschema.TimeFormat))
	if cerr != nil {
		http.Error(w, "audit unavailable", http.StatusInternalServerError)
		return
	}
	var rejected int64
	data.EventCounts = make([]components.SecurityEventCount, 0, len(counts))
	for _, count := range counts {
		data.EventCounts = append(data.EventCounts, components.SecurityEventCount{
			Event: count.Event, Reason: count.Reason, Count: count.Count,
		})
		if strings.Contains(count.Event, "rejected") || strings.Contains(count.Event, "throttled") {
			rejected += count.Count
		}
	}
	const auditRejectAlertThreshold = 20
	if rejected >= auditRejectAlertThreshold {
		data.AuditAlert = "High rejected-authentication volume in the last 24h."
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
	h.auth.RecordAudit(r.Context(), auth.EventAdminRoleChanged,
		"role="+role, email, target.ID, h.actorID(r))
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
	// RevokeAllSessions also drops pending magic links: an admin "revoke" must
	// not leave a 15-minute login credential alive.
	if err := h.auth.RevokeAllSessions(r.Context(), user.ID); err != nil {
		redirectConfig(w, r, "error", "Could not revoke sessions.")
		return
	}
	h.auth.RecordAudit(r.Context(), auth.EventAdminSessionsRevoked,
		"admin action", email, user.ID, h.actorID(r))
	redirectConfig(w, r, "notice", "Sessions revoked.")
}

// handleAuditCSV exports the recent audit trail for an incident review. The
// same rows the panel renders, escaped by encoding/csv, no token or password
// by construction. Bounded like the panel (newest 5000).
func (h *Handler) handleAuditCSV(w http.ResponseWriter, r *http.Request) {
	const auditExportLimit = 5000
	rows, err := h.queries.ListSecurityEvents(r.Context(), auditExportLimit)
	if err != nil {
		http.Error(w, "audit unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="security-events.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"created", "event", "reason", "email", "user_id", "actor_id"})
	for _, row := range rows {
		_ = cw.Write([]string{
			csvCell(row.Created), csvCell(row.Event), csvCell(row.Reason),
			csvCell(row.Email), csvCell(row.UserID), csvCell(row.ActorID),
		})
	}
	cw.Flush()
}

// formatWhen renders a stored timestamp for the panel; the CSV export keeps
// the raw ISO value so an external tool can sort it.
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

// csvCell neutralises spreadsheet formula injection: an email or reason that
// starts with =, +, -, @ (or a control prefix) is data, never a formula.
func csvCell(v string) string {
	if v == "" {
		return v
	}
	switch v[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + v
	}
	return v
}

// actorID is the signed-in operator behind an admin mutation. RequireAdmin
// already proved the session exists, so a miss here is a wiring bug, not an
// authorization decision: record the action with an empty actor rather than
// dropping the audit row.
func (h *Handler) actorID(r *http.Request) string {
	sess, err := h.auth.Session(r)
	if err != nil || sess == nil || sess.User == nil {
		return ""
	}
	return sess.User.ID
}

func redirectConfig(w http.ResponseWriter, r *http.Request, key, msg string) {
	http.Redirect(w, r, "/config?"+key+"="+url.QueryEscape(msg), http.StatusSeeOther)
}
