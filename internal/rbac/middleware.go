// SCOPE:core — RBAC HTTP middleware (session role -> policy decision).
package rbac

import (
	"log/slog"
	"net"
	"net/http"

	"github.com/casbin/casbin/v2"

	"github.com/calionauta/gogogo/internal/auth"
)

// objectOf builds the ABAC request object: the path plus the identity and
// client address a code-defined condition can read. Owner is left empty
// here (a per-record loader fills it when a surface needs ownership), so
// the default decision is unchanged. The model's attrOK clause ignores an
// ownerless object.
func objectOf(r *http.Request, sess *auth.ValidatedSession, serr error) Object {
	o := Object{Path: r.URL.Path, IP: clientIP(r)}
	if serr == nil && sess != nil && sess.User != nil {
		o.SubjectID = sess.User.ID
	}
	return o
}

// clientIP is the request's client address without the port, for a
// future code-defined IP condition.
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// Middleware enforces the embedded policy, built once and reused
// (EnforceWith hot path). Disabled is a pass-through with the same call
// shape — the wiring stays identical, only the decision changes — so
// flipping RBAC_ENABLED never rewires the mux. Denies answer 403 with no
// body to mine (same for policy errors: fail closed, no oracle); every
// deny is a warn log with subject/object/action (the audit trail auth's
// securitylog starts, enforcement completes).
func Middleware(a *auth.Auth, e *casbin.Enforcer, enabled bool) func(http.Handler) http.Handler {
	return MiddlewareWithPermissive(a, e, enabled, false)
}

// MiddlewareWithPermissive adds audit-before-enforce rollout: with
// permissive true, would-be denies are warn-logged (with permissive=1)
// and the request proceeds. Flip it off once the logs are clean —
// see docs/rbac.md. Production posture is permissive=false.
func MiddlewareWithPermissive(
	a *auth.Auth, e *casbin.Enforcer, enabled, permissive bool,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enabled {
				next.ServeHTTP(w, r)
				return
			}
			sess, serr := a.Session(r)
			sub := subjectOf(sess, serr)
			allow, err := EnforceWith(e, sub, objectOf(r, sess, serr), r.Method)
			if err != nil {
				slog.Warn("rbac: enforcer error, denying",
					"sub", sub, "obj", r.URL.Path, "act", r.Method, "error", err)
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if !allow {
				if permissive {
					slog.Warn("rbac: permissive allow (would deny)",
						"sub", sub, "obj", r.URL.Path, "act", r.Method, "permissive", 1)
					next.ServeHTTP(w, r)
					return
				}
				slog.Warn("rbac: denied",
					"sub", sub, "obj", r.URL.Path, "act", r.Method)
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
