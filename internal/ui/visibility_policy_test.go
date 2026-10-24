// SCOPE:core — Nav/policy parity: no offered link 403s.
//
// Every module Visible() returns for a role must be reachable by that role
// under the compiled RBAC policy. A policy edit that removes a grant (or a
// Visible edit that adds a link) fails here naming the pair — the same
// anti-drift role coverage_test.go plays for routes. Guest is excluded: the
// shell never renders for guests (landing links route through login, pinned
// in features/authviews).
package ui

import (
	"testing"

	"github.com/calionauta/gogogo/internal/rbac"
)

// TestVisibleModulesArePolicyReachable fails when the sidebar offers a role a
// destination its policy denies — the click would land on a bare 403.
func TestVisibleModulesArePolicyReachable(t *testing.T) {
	e, err := rbac.NewEnforcer()
	if err != nil {
		t.Fatalf("new enforcer: %v", err)
	}
	for _, role := range []string{"admin", "user", "viewer"} {
		for _, m := range Visible(role) {
			allow, err := rbac.EnforceWith(e, role, m.URL, "GET")
			if err != nil {
				t.Fatalf("enforce %s GET %s: %v", role, m.URL, err)
			}
			if !allow {
				t.Errorf("role %q is offered %q (%s) but the policy denies GET %s",
					role, m.Title, m.URL, m.URL)
			}
		}
	}
}
