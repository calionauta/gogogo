// SCOPE:core — RBAC coverage gate over the composed router.
//
// Two inventories answer the same question, and they answer it at different
// times. The source scan (coverage_test.go) reads `r.Get("/path")` calls before
// anything boots, which is what keeps a trimmed tree checkable without its
// dependencies. This one walks a router that is already composed, and that is
// the only inventory that can see a route mounted at RUNTIME — a recipe's
// routes never appear in a file scan, so a source-only gate would stay green
// while losing coverage the moment extensions mount through the seam.
package rbac

import (
	"fmt"
	"net/http"
	"slices"

	"github.com/casbin/casbin/v3"
	"github.com/go-chi/chi/v5"
)

// Route is one method+pattern the composed tree serves.
type Route struct {
	Method  string
	Pattern string
}

// RoutesWalked lists what root actually serves. HEAD is omitted: it rides GET
// in every router here, and the policy matrix is written per GET path.
func RoutesWalked(root chi.Router) ([]Route, error) {
	var out []Route
	if err := chi.Walk(root, func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == http.MethodHead || pattern == "" {
			return nil
		}
		out = append(out, Route{Method: method, Pattern: pattern})
		return nil
	}); err != nil {
		return nil, fmt.Errorf("rbac: walk routes: %w", err)
	}
	return out, nil
}

// RoutePolicyProblems walks root and reports every route whose policy decision
// is missing or wrong: a non-public route must allow `user` on its own (admin's
// `/*` row must not be what reaches it), a declared-public route must allow
// anonymous, and a declared-admin-only route must deny `user` while answering
// admin.
//
// Callers pass the REAL composition tree. That is what makes a recipe route
// covered by construction: if it mounts, it is checked, with no manifest line
// to forget.
func RoutePolicyProblems(root chi.Router) ([]string, error) {
	e, err := NewEnforcer()
	if err != nil {
		return nil, fmt.Errorf("rbac: enforcer: %w", err)
	}
	routes, err := RoutesWalked(root)
	if err != nil {
		return nil, err
	}
	return policyProblems(e, routes), nil
}

// policyProblems is the enforcement half, split out so a caller that already
// has a route list (the source scan) can share it.
//
// The decision is made per PATTERN, not per method, and that is the semantics
// the policy is written in: "object = request path, action = method; method
// checks inside feature routers stay theirs". A leaf handler mounted with
// r.Handle answers every verb, so demanding one row per verb would demand rows
// the edge does not decide. A pattern missing from the policy still fails
// here — which is the failure mode this gate exists for.
func policyProblems(e *casbin.Enforcer, routes []Route) []string {
	byPattern := make(map[string][]string, len(routes))
	patterns := make([]string, 0, len(routes))
	for _, rt := range routes {
		if rt.Pattern == "" {
			continue
		}
		if _, seen := byPattern[rt.Pattern]; !seen {
			patterns = append(patterns, rt.Pattern)
		}
		byPattern[rt.Pattern] = append(byPattern[rt.Pattern], rt.Method)
	}
	slices.Sort(patterns)

	out := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		out = append(out, patternProblems(e, pattern, byPattern[pattern])...)
	}
	return out
}

// patternProblems decides one pattern: an admin-only route must deny user and
// answer admin, a declared-public route must answer anonymous, and everything
// else must answer BOTH user and admin.
func patternProblems(e *casbin.Enforcer, pattern string, methods []string) []string {
	allows := func(role string) (bool, error) {
		for _, m := range methods {
			ok, err := e.Enforce(role, pattern, m)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
	allowUser, err := allows(RoleUser)
	if err != nil {
		return []string{pattern + ": enforce user: " + err.Error()}
	}
	allowAdmin, err := allows(RoleAdmin)
	if err != nil {
		return []string{pattern + ": enforce admin: " + err.Error()}
	}
	switch {
	case AdminOnlyRoute(pattern):
		var out []string
		if allowUser {
			out = append(out, pattern+": admin-only route, but the policy grants it to user")
		}
		if !allowAdmin {
			out = append(out, pattern+": admin-only route denied for admin too — unreachable")
		}
		return out
	case PublicRoute(pattern):
		allowAnon, aerr := allows(RoleAnonymous)
		if aerr != nil {
			return []string{pattern + ": enforce anonymous: " + aerr.Error()}
		}
		if !allowAnon {
			return []string{pattern + ": declared public but anonymous is denied"}
		}
		return nil
	default:
		// Testing user||admin lets `p, admin, /*, .*` mask a missing user row —
		// which is how a realtime probe once shipped broken: admin reached it,
		// so nothing looked wrong, while every signed-in non-admin got a 403.
		var out []string
		if !allowUser {
			out = append(out, pattern+": no `user` policy row reaches this route "+
				"(admin's /* masks it, which is not a decision for users). "+
				"Add rows to casbin/policy.csv and, only if the route must stay "+
				"admin-only, to the adminOnlyRoutes table.")
		}
		if !allowAdmin {
			out = append(out, pattern+": admin cannot reach a non-admin-only route — check the /* row")
		}
		return out
	}
}
