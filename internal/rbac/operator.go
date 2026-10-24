// SCOPE:core — Always-on fence for operator surfaces.
//
// The Casbin policy is opt-in (RBAC_ENABLED=false by default). That is a
// deliberate single-tenant default, but it cannot mean "operator surfaces
// are public": /config, /console and /dagnats expose runtime topology and a
// workflow console. When the policy is off there is no subject resolution
// left, so this middleware answers the one question those routes need from
// the session itself: is this user an admin?
//
// It is deliberately tiny and lives here, next to IsAdmin, so a role
// comparison never reappears scattered through feature handlers. Policy
// still owns every ordinary route decision.
package rbac

import (
	"net/http"

	"github.com/calionauta/gogogo/internal/auth"
)

// RequireAdmin denies anyone who is not an admin. A missing session is a
// 403, not a redirect: these are operator endpoints, not pages a browser
// should be bounced through login for. The response carries no body.
func RequireAdmin(a *auth.Auth) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess, err := a.Session(r)
			if err != nil || sess == nil || sess.User == nil || !IsAdmin(sess.Role) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
