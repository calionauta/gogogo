// SCOPE:core — every mutating route refuses an anonymous caller.

package rbac

import (
	"regexp"
	"strings"
	"testing"
)

// TestNoMutatingRouteIsAnonymous is the guard for the class of bug that put a
// cookie-less write into a collaborator's doc: an endpoint that mutates state
// and has no `anonymous` row in the policy.
//
// It reads the source (like the coverage test) so it needs no feature imports
// and survives every trimmed tree. The rule is deliberately blunt: a mutating
// verb on a path the policy lets `anonymous` reach is a finding, unless the
// path is one of the two explicitly-public writes below.
//
// The corollary — that the HANDLER also checks the session — cannot be proven
// from the policy alone (RBAC_ENABLED defaults to false, so the handler is the
// only guard on a stock deployment). That half is pinned by each feature's
// acceptance test: TestNotesRejectsAnonymousAccess,
// TestWhiteboardRejectsAnonymousAccess, and the todo handler's anon redirects.
func TestNoMutatingRouteIsAnonymous(t *testing.T) {
	e, err := NewEnforcer()
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	// Public writes by contract: signup/signin MUST work without a session,
	// the session probe is a read-shaped POST-less GET, and the Stripe
	// webhook authenticates by signature rather than by session.
	publicWrites := map[string]bool{
		// Signed-out by design: each of these must work before a session exists.
		"/api/auth/signup/credential":      true,
		"/api/auth/signin/credential":      true,
		"/api/auth/magic/request":          true,
		"/api/auth/magic":                  true, // consumes a one-time emailed token
		"/api/auth/verify/request":         true, // anti-enumeration verification request
		"/api/auth/password/reset/request": true, // anti-enumeration request
		"/api/auth/password/reset":         true, // token-authenticated reset
		"/api/auth/totp/login":             true,
		"/api/auth/signout":                true, // logging out must work with an expired session
		"/login":                           true, // the credential form itself
		"/logout":                          true,
	}

	var findings []string
	for _, rt := range registeredRoutes(t) {
		for _, method := range rt.methods {
			switch method {
			case "GET", "HEAD", "OPTIONS":
				continue // reads: anonymous access is a policy decision, not a hole
			}
			if publicWrites[rt.pattern] {
				continue
			}
			allowAnon, aerr := e.Enforce(RoleAnonymous, rt.pattern, method)
			if aerr != nil {
				t.Fatalf("enforce anonymous %s %s: %v", method, rt.pattern, aerr)
			}
			if allowAnon {
				findings = append(findings, method+" "+rt.pattern)
			}
		}
	}
	for _, f := range findings {
		t.Errorf("%s: reachable without a session — either drop the anonymous grant, "+
			"add the path to publicWrites (with the reason), or make the handler "+
			"require a session", f)
	}
}

// TestMutatingRouteScanIsNotVacuous proves the sweep above actually inspected
// mutating routes. A scanner that silently matched nothing would leave the
// guard green forever — the failure mode this whole file exists to avoid.
func TestMutatingRouteScanIsNotVacuous(t *testing.T) {
	routes := registeredRoutes(t)
	mutating := 0
	for _, rt := range routes {
		for _, m := range rt.methods {
			if m != "GET" && m != "HEAD" && m != "OPTIONS" {
				mutating++
			}
		}
	}
	if mutating < 15 {
		t.Fatalf("found only %d mutating routes across %d patterns — "+
			"the scanner is broken, not the tree safe", mutating, len(routes))
	}
	if len(routes) == 0 {
		t.Fatal("no routes scanned at all")
	}
}

// TestAnonymousPolicyIsMinimal pins the OTHER direction of the same rule: an
// `anonymous` grant must stay a small, reviewed set. A wildcard that quietly
// widened (a new `/api/notifications/*` row added "to unblock the demo")
// would be invisible in review but obvious here.
func TestAnonymousPolicyIsMinimal(t *testing.T) {
	var anon []string
	for line := range strings.SplitSeq(Policy(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) != policyFields {
			continue
		}
		if strings.TrimSpace(parts[1]) == RoleAnonymous {
			anon = append(anon, strings.TrimSpace(parts[2]))
		}
	}
	const maxAnonymousObjects = 20
	if len(anon) > maxAnonymousObjects {
		t.Errorf("the anonymous block has %d objects (max %d): is every one still public by intent?\n%v",
			len(anon), maxAnonymousObjects, anon)
	}
	// No blanket wildcard: `/api/*` for anonymous would publish the app.
	for _, obj := range anon {
		if regexp.MustCompile(`^/(api|\*)$`).MatchString(obj) {
			t.Errorf("anonymous grant %q is a blanket wildcard — publish specific routes instead", obj)
		}
	}
}
