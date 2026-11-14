// The root route: send a visitor to the right place instead of rendering a
// page nobody asked for.
//
// There used to be a `landing` unit here — a public page whose whole content
// was a CTA. That is a surface with no owner: for a signed-in user it repeats
// navigation the shell already renders, and for a guest the only thing to do
// is sign in. So `/` is core now, and it answers with a redirect:
//
//   - no session → /login, which is the real public entry;
//   - session    → the app's home, resolved from what THIS build actually
//     mounts (dashboard first, then the first destination the role may open).
//
// The resolution is not decoration: dashboard is a recipe a product can
// uninstall, and a hand-hardcoded /dashboard would 404 the root of that tree.
// Reading the mounted set keeps `/` correct in a trimmed checkout with no
// second list to maintain.
package server

import (
	"net/http"

	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/ui"
)

// homePath is the preferred signed-in destination: the summary page, which is
// also the host of the dashboard.card slot. Products that uninstall it get the
// first destination their role can open instead.
const homePath = "/dashboard"

// loginPath is where a visitor with no session is sent. The login flow owns
// the post-sign-in hop, so the root does not need a `next` here.
const loginPath = "/login"

// mountHome claims and mounts the root route. It lives here so the composition
// root names one line, and the claim cannot drift from the handler it guards.
func mountHome(r *Router, a *auth.Auth) {
	r.Claim("core", http.MethodGet, "/")
	r.Get("/", homeHandler(a, r))
}

// homeHandler answers GET / with a redirect. It is deliberately stateless: the
// mounted set is read from the composed router, so nothing has to be kept in
// sync when a unit is uninstalled.
func homeHandler(a *auth.Auth, r *Router) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		role := ""
		if sess, err := a.Session(req); err == nil && sess.User != nil {
			role = sess.Role
		} else {
			http.Redirect(w, req, loginPath, http.StatusSeeOther)
			return
		}
		// The target is a path from a closed set (homePath or a ui.Module URL),
		// never client input: the role only SELECTS from that set.
		http.Redirect(w, req, homeFor(role, r), http.StatusSeeOther) //nolint:gosec // G710: closed set, not user input
	}
}

// homeFor resolves the signed-in home for a role.
//
// Order matters and is the whole contract: the dashboard when it is mounted,
// otherwise the first destination the rail would offer this role, otherwise
// the account page (always present: it is where a session can be managed even
// in the smallest tree). A destination is "mounted" when the composed tree
// serves GET on its path.
func homeFor(role string, r *Router) string {
	if r.serves(http.MethodGet, homePath) {
		return homePath
	}
	for _, m := range ui.Visible(role) {
		if r.serves(http.MethodGet, m.URL) {
			return m.URL
		}
	}
	return "/account"
}
