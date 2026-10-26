// SCOPE:core — RBAC HTTP middleware (session role -> policy decision).
package rbac

import (
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/casbin/casbin/v3"

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

// loginRedirect is where a signed-out browser lands: the login page, carrying
// the page it asked for so sign-in returns there. auth=required is what makes
// the page explain WHY it appeared ("sign in to continue") instead of looking
// like the user navigated to /login on purpose.
//
// Only the request's own path+query is carried, and a value that is not a
// same-origin path is replaced with "/" before it reaches the URL; the login
// handler applies the same check again (loginNext), so `next` can never
// become an open redirect.
func loginRedirect(r *http.Request) string {
	next := r.URL.RequestURI()
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = "/"
	}
	return "/login?next=" + url.QueryEscape(next) + "&auth=required"
}

// isPageNavigation reports whether the request is a browser asking for a PAGE
// (typed URL, bookmark, link) rather than a script asking for DATA.
//
// A signed-out person who opens /chat should see the login form, not a bare
// "forbidden" they cannot act on. An XHR/SSE/Datastar request must NOT get the
// HTML login page — it gets a 401 the client can act on.
//
// Sec-Fetch-Mode is the reliable signal (navigate vs cors/same-origin/
// no-cors). Older clients and tests omit it; the Accept fallback then applies,
// excluding Datastar requests and event streams, whose Accept also names
// text/html.
func isPageNavigation(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if mode := r.Header.Get("Sec-Fetch-Mode"); mode != "" {
		return mode == "navigate"
	}
	if r.Header.Get("Datastar-Request") != "" {
		return false
	}
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "text/html") && !strings.Contains(accept, "text/event-stream")
}

// denyAnonymous answers an unauthenticated request that the policy refused:
// a page navigation is bounced to /login with an explanation, anything else
// gets a 401 saying sign-in is required. Neither response reveals whether the
// path exists — the decision is not a 404/403 oracle.
func denyAnonymous(w http.ResponseWriter, r *http.Request) {
	if isPageNavigation(r) {
		// #nosec G710 -- same-origin PATH only: loginRedirect drops anything
		// that is not a single-slash path, and loginNext re-validates before
		// the post-login jump. The query is URL-encoded, so it cannot smuggle
		// a scheme or host.
		http.Redirect(w, r, loginRedirect(r), http.StatusSeeOther)
		return
	}
	http.Error(w, "sign-in required", http.StatusUnauthorized)
}

// Middleware enforces the embedded policy, built once and reused
// (EnforceWith hot path). Disabled is a pass-through with the same call
// shape — the wiring stays identical, only the decision changes — so
// flipping RBAC_ENABLED never rewires the mux.
//
// A deny answers differently by audience: an anonymous PAGE navigation is
// bounced to /login (with the requested path and an explanation), an
// anonymous data request gets 401 "sign-in required", and a signed-in user
// the policy refused gets 403 with no body to mine (same for policy errors:
// fail closed, no oracle). Every deny is a warn log with
// subject/object/action (the audit trail auth's securitylog starts,
// enforcement completes).
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
				if sub == RoleAnonymous {
					denyAnonymous(w, r)
					return
				}
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
				if sub == RoleAnonymous {
					denyAnonymous(w, r)
					return
				}
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
