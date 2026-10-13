// SCOPE:core — RBAC HTTP middleware (session role -> policy decision).
package rbac

import (
	"log/slog"
	"net/http"

	"github.com/casbin/casbin/v2"

	"github.com/calionauta/gogogo/internal/auth"
)

// roleAnonymous is the subject for requests without a valid session.
// roleUser is the fallback for authenticated users whose row carries an
// unknown role value: least surprise (they keep working), documented in
// the migration that added the column.
const (
	roleAnonymous = "anonymous"
	roleUser      = "user"
	roleAdmin     = "admin"
	// RoleViewer is the restricted demo role: read + create + toggle on
	// todos, no deletes, no paid actions. Resolved from users.role like
	// admin; unknown values still fall back to user below.
	RoleViewer = "viewer"
)

// IsViewer reports whether a session role gets the restricted demo
// surface (disabled delete affordances + message instead of modal).
// This is the reusable permission check: display hint only —
// enforcement stays in policy.csv, so a wrong answer here degrades to
// a 403, never to an unauthorized write.
func IsViewer(role string) bool {
	return role == RoleViewer
}

// subjectOf resolves the decision subject: the session's role, "user" for
// anything unrecognized, "anonymous" without a session. Unknown values
// must not lock users out over a typo in the column — and must never
// escalate: only the exact strings "admin" and "viewer" leave the user
// default, and viewer can only lose permissions, never gain.
func subjectOf(sess *auth.ValidatedSession, err error) string {
	if err != nil || sess == nil {
		return roleAnonymous
	}
	if sess.Role == roleAdmin {
		return roleAdmin
	}
	if sess.Role == RoleViewer {
		return RoleViewer
	}
	return roleUser
}

// Middleware enforces the embedded policy, built once and reused
// (EnforceWith hot path). Disabled is a pass-through with the same call
// shape — the wiring stays identical, only the decision changes — so
// flipping RBAC_ENABLED never rewires the mux. Denies answer 403 with no
// body to mine (same for policy errors: fail closed, no oracle); every
// deny is a warn log with subject/object/action (the audit trail auth's
// securitylog starts, enforcement completes).
func Middleware(a *auth.Auth, e *casbin.Enforcer, enabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enabled {
				next.ServeHTTP(w, r)
				return
			}
			sess, serr := a.Session(r)
			sub := subjectOf(sess, serr)
			allow, err := EnforceWith(e, sub, r.URL.Path, r.Method)
			if err != nil {
				slog.Warn("rbac: enforcer error, denying",
					"sub", sub, "obj", r.URL.Path, "act", r.Method, "error", err)
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if !allow {
				slog.Warn("rbac: denied",
					"sub", sub, "obj", r.URL.Path, "act", r.Method)
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
