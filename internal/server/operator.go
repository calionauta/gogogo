// SCOPE:core — Seeds ui.WithOperator for page navigations.
//
// The shell's account menu shows the DagNats console entry only when the
// console would actually answer: a workflow engine was STARTED in this binary,
// the session is an admin, and its address is listed in ADMIN_EMAILS. The rule
// belongs to one place — rbac.IsOperator plus the engine's own wiring — and the
// composition root resolves it once per page so a feature template never reads
// config or repeats the predicate.
//
// It sits with the other page-scoped seeders (see inbox_badge.go) because both
// answer the same question for the same rail: what does this session see?
package server

import (
	"net/http"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/rbac"
	"github.com/calionauta/gogogo/internal/ui"
)

// shellMiddleware composes the two page-scoped seeders — the rail's unread
// count and the operator flag — so the composition root states the order once
// and every shell render gets both before the page runs.
func shellMiddleware(
	cfg *config.Config, a *auth.Auth, q *dbgen.Queries, engine engineWired, next http.Handler,
) http.Handler {
	return perfPageMiddleware(cfg)(operatorPageMiddleware(cfg, a, engine)(inboxBadgeMiddleware(a, q)(next)))
}

// perfPageMiddleware seeds ui.WithPerf on page navigations when the runtime
// dashboard is enabled. It needs no session: the menu entry also requires
// Operator(ctx), which the operator middleware decides, so this flag alone
// never offers the link.
func perfPageMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	if cfg == nil || !cfg.PerfEnabled {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if pageRequest(r) {
				r = r.WithContext(ui.WithPerf(r.Context(), true))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// engineWired answers "did this binary start a workflow engine?". It is the
// build/runtime truth a config flag cannot give: a trimmed tree keeps
// DAGNATS_ENABLED's default while startDagNats is gone, and the console mount
// is deliberately NOT trimmed (it is the scaffold's chi.go anchor), so a
// config-only gate would offer a link to a proxy that answers 502.
type engineWired interface {
	Wired() bool
}

// operatorPageMiddleware seeds ui.WithOperator on shell renders. It is a
// pass-through when the console cannot exist at all (no admin list), so an app
// without an operator pays no session read per navigation.
func operatorPageMiddleware(cfg *config.Config, a *auth.Auth, engine engineWired) func(http.Handler) http.Handler {
	if cfg == nil || engine == nil || len(cfg.AdminEmails) == 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Wired is read per request, not captured: BootChi runs before
			// startDagNats hands the run client over.
			if !pageRequest(r) || !engine.Wired() {
				next.ServeHTTP(w, r)
				return
			}
			sess, err := a.Session(r)
			if err == nil && sess != nil && sess.User != nil &&
				rbac.IsAdmin(sess.Role) && rbac.IsOperator(cfg.AdminEmails, sess.User.Email) {
				r = r.WithContext(ui.WithOperator(r.Context(), true))
			}
			next.ServeHTTP(w, r)
		})
	}
}
