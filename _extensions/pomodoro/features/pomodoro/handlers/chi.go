// The pomodoro recipe's HTTP slice: one page, one write.
package handlers

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/pomodoro/components"
	"github.com/calionauta/gogogo/internal/auth"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/observability"
)

// Handler serves the recipe's surface. Its dependencies arrive through
// register.Deps, the same values the root hands its own units, so a recipe is
// not a special case at boot.
type Handler struct {
	auth *auth.Auth
	q    *dbgen.Queries
	cfg  *config.Config
}

// NewHandler builds the slice. A missing auth or store is a wiring bug, and the
// recipe would otherwise dereference it on the first request.
func NewHandler(a *auth.Auth, q *dbgen.Queries, cfg *config.Config) *Handler {
	if a == nil || q == nil {
		panic("pomodoro: NewHandler needs auth and queries")
	}
	return &Handler{auth: a, q: q, cfg: cfg}
}

// Routes returns the recipe's router with ABSOLUTE patterns, the convention the
// composition root flattens through MountFeature. The patterns must match the
// declaration in .recipe.yml: install checks that they do.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/pomodoro", h.page)
	r.Post("/api/pomodoro/sessions", h.create)
	return r
}

// session resolves the caller, or sends them to the login page with the reason.
//
// The handler checks the session itself. The RBAC policy grants the path, not
// the data: with RBAC_ENABLED=false (a development default) the policy decides
// nothing at all, and a page that reads one caller's rows without asking who
// they are is the bug this exists to prevent.
func (h *Handler) session(w http.ResponseWriter, r *http.Request) (*auth.ValidatedSession, bool) {
	sess, err := h.auth.Session(r)
	if err != nil || sess == nil {
		target := "/login?next=" + url.QueryEscape(r.URL.RequestURI()) + "&auth=required"
		http.Redirect(w, r, target, http.StatusSeeOther)
		return nil, false
	}
	return sess, true
}

// page renders the list of the caller's own sessions.
func (h *Handler) page(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(w, r)
	if !ok {
		return
	}
	rows, err := h.q.ListPomodoroSessions(r.Context(), sess.User.ID)
	if err != nil {
		// Not a fake empty list: an unrunnable query would render as "no sessions
		// yet", which is indistinguishable from the real thing.
		observability.Error(r.Context(), "pomodoro: list sessions failed",
			append(observability.WithComponent("pomodoro", "handler"), "error", err)...)
		http.Error(w, "listing sessions failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = components.Page(components.PageData{
		Sessions:    sessionViews(rows),
		Email:       sess.User.Email,
		Role:        sess.Role,
		BuildLabel:  h.cfg.BuildLabel,
		BuildCommit: h.cfg.BuildCommit,
	}).Render(r.Context(), w)
}

// create records one session and redirects back to the page, so a reload is the
// same request as the write's outcome: no second state to keep in step.
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	label := strings.TrimSpace(r.PostFormValue("label"))
	if label == "" {
		http.Error(w, "a session needs a label", http.StatusBadRequest)
		return
	}
	minutes, err := strconv.Atoi(r.PostFormValue("minutes"))
	if err != nil || minutes < 1 || minutes > 180 {
		minutes = 25
	}
	insErr := h.q.CreatePomodoroSession(r.Context(), dbgen.CreatePomodoroSessionParams{
		ID:        dbschema.NewID(),
		Owner:     sess.User.ID,
		Label:     label,
		Minutes:   int64(minutes),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	if insErr != nil {
		observability.Error(r.Context(), "pomodoro: create session failed",
			append(observability.WithComponent("pomodoro", "handler"), "error", insErr)...)
		http.Error(w, "recording the session failed", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/pomodoro", http.StatusSeeOther)
}

// sessionViews converts stored rows into what the page renders, formatting the
// timestamp once and in one place.
func sessionViews(rows []dbgen.ExtPomodoroSession) []components.Session {
	out := make([]components.Session, 0, len(rows))
	for _, row := range rows {
		out = append(out, components.Session{
			Label:   row.Label,
			Minutes: row.Minutes,
			When:    displayTime(row.CreatedAt),
		})
	}
	return out
}

// displayTime shortens an RFC3339 stamp to "YYYY-MM-DD HH:MM" for the list. A
// value that does not parse is shown as stored: a timestamp the page cannot read
// is still a fact the user should see, not a blank.
func displayTime(stamp string) string {
	if len(stamp) < 16 {
		return stamp
	}
	if _, err := time.Parse(time.RFC3339, stamp); err != nil {
		return stamp
	}
	return stamp[:10] + " " + stamp[11:16]
}
