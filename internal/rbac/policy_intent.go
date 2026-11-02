// SCOPE:core — RBAC policy intent: which routes are admin-only, which are public.

package rbac

import "slices"

// adminOnlyRoutes declares the routes the policy deliberately keeps away from
// a plain `user`. It is the ONE place that decision is written down, so a
// reviewer sees it in a diff instead of inferring it from an absent policy row.
var adminOnlyRoutes = map[string]bool{
	"/config":              true,
	"/config/":             true,
	"/config/*":            true,
	"/config/users/role":   true,
	"/config/users/revoke": true,
	"/config/audit.csv":    true,
	"/debug/statsviz":      true,
	"/debug/statsviz/*":    true,
	"/debug/statsviz/ws":   true,
	"/debug/pprof":         true,
	"/debug/pprof/*":       true,
}

// publicRoutes declares the routes a signed-out visitor may reach. An
// accidentally-public route (reachable through a wildcard nobody meant to
// widen) fails a gate instead of shipping.
var publicRoutes = map[string]bool{
	"/":                                   true,
	"/login":                              true,
	"/signup":                             true,
	"/activate":                           true,
	"/logout":                             true,
	"/health":                             true,
	"/sw.js":                              true,
	"/api/auth/signup/credential":         true,
	"/api/auth/signin/credential":         true,
	"/api/auth/signout":                   true,
	"/api/auth/magic":                     true,
	"/api/auth/magic/request":             true,
	"/api/auth/oauth/{provider}":          true,
	"/api/auth/oauth/{provider}/callback": true,
	"/api/auth/totp/login":                true,
	"/api/auth/verify":                    true,
	"/api/auth/verify/request":            true,
	"/api/auth/password/reset":            true,
	"/api/auth/password/reset/request":    true,
	"/api/session/stream":                 true,
	// Browser CSP reports post with no session. The row is anonymous by
	// policy; naming it here is what makes the coverage gate agree, and it
	// is mounted by a CONSTANT path, so the source scan cannot see it.
	"/api/security/csp-report": true,
}

// AdminOnlyRoute reports whether pattern is declared admin-only.
func AdminOnlyRoute(pattern string) bool { return adminOnlyRoutes[pattern] }

// PublicRoute reports whether pattern is declared reachable without a session.
func PublicRoute(pattern string) bool { return publicRoutes[pattern] }

// PublicRoutePatterns lists the declared-public patterns in sorted order, so a
// gate can check every one of them without reaching into the map.
func PublicRoutePatterns() []string {
	out := make([]string, 0, len(publicRoutes))
	for p := range publicRoutes {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}
