// SCOPE:core — Shell registry contract.
package ui

import "testing"

// adminOnlyIDs are the modules whose ROUTES are admin-only in the RBAC policy
// (internal/auth policy + the admin gate on /config). Kept explicit, not read
// back from the Module.Admin flag: a test that consults the same field the
// sidebar filters by cannot notice the flag being dropped — it was written that
// way first, and red-proofing showed it stayed green with `Admin: true` deleted.
var adminOnlyIDs = map[string]bool{"config": true}

// TestGroupedHidesAdminModulesFromNonAdmins pins the only role rule the
// sidebar has: an admin-only module is invisible to a viewer or a plain user,
// and visible to an admin. The route stays RBAC-enforced — this is the
// affordance, and a mismatch between the two is how a demo renders a link that
// 403s.
func TestGroupedHidesAdminModulesFromNonAdmins(t *testing.T) {
	for _, role := range []string{"", "user", "viewer"} {
		for _, g := range Grouped(role) {
			for _, m := range g.Modules {
				if adminOnlyIDs[m.ID] {
					t.Errorf("role %q sees admin-only module %q", role, m.ID)
				}
			}
		}
	}

	var adminSees bool
	for _, g := range Grouped("admin") {
		for _, m := range g.Modules {
			if adminOnlyIDs[m.ID] {
				adminSees = true
			}
		}
	}
	if !adminSees {
		t.Fatal("admin does not see an admin-only module")
	}
}

// TestAdminFlagMatchesAdminOnlyRoutes keeps the two sources honest: a module
// whose route is admin-gated must carry Admin, and a module carrying Admin must
// be a route the policy actually gates. Adding an admin surface means touching
// both — which is the review moment this exists for.
func TestAdminFlagMatchesAdminOnlyRoutes(t *testing.T) {
	for _, m := range Modules {
		if m.Admin && !adminOnlyIDs[m.ID] {
			t.Errorf("module %q carries Admin but its route is not policy-gated: "+
				"either the module should not hide, or adminOnlyIDs needs it", m.ID)
		}
		if adminOnlyIDs[m.ID] && !m.Admin {
			t.Errorf("module %q is admin-only in the RBAC policy but the sidebar does not hide it", m.ID)
		}
	}
}

// TestVisibleViewerSeesCollabOnly pins the viewer affordance: the viewer
// policy denies the AI + Room + operator paths, so offering them would render
// destinations that 403 on open. Plain users keep every non-admin module.
func TestVisibleViewerSeesCollabOnly(t *testing.T) {
	ids := map[string]bool{}
	for _, g := range Grouped("viewer") {
		for _, m := range g.Modules {
			ids[m.ID] = true
		}
	}
	for _, want := range []string{"todo", "whiteboard", "notes"} {
		if !ids[want] {
			t.Errorf("viewer does not see %q (viewer policy allows it)", want)
		}
	}
	for _, banned := range []string{"chat", "review", "room", "config"} {
		if ids[banned] {
			t.Errorf("viewer sees %q (viewer policy denies it: click 403s)", banned)
		}
	}

	userIDs := map[string]bool{}
	for _, g := range Grouped("user") {
		for _, m := range g.Modules {
			userIDs[m.ID] = true
		}
	}
	for _, want := range []string{"todo", "whiteboard", "notes", "chat", "review", "room"} {
		if !userIDs[want] {
			t.Errorf("user does not see %q", want)
		}
	}
}

// TestGroupedKeepsEveryModuleVisibleSomewhere fails when a module is added to
// the registry without a group the shell renders — the module would exist as a
// route with no way to reach it (a dropped scope, not a layout choice).
func TestGroupedKeepsEveryModuleVisibleSomewhere(t *testing.T) {
	seen := map[string]bool{}
	for _, g := range Grouped("admin") {
		if g.Label == "" {
			t.Errorf("group with modules but no label: %+v", g.Modules)
		}
		for _, m := range g.Modules {
			seen[m.ID] = true
			if m.URL == "" || m.Title == "" || m.Icon == "" {
				t.Errorf("module %q is missing URL/Title/Icon", m.ID)
			}
		}
	}
	for _, m := range Modules {
		if !seen[m.ID] {
			t.Errorf("module %q is unreachable: no group renders it", m.ID)
		}
	}
}

// TestByIDResolvesEveryModule keeps the header title lookup honest.
func TestByIDResolvesEveryModule(t *testing.T) {
	for _, m := range Modules {
		got, ok := ByID(m.ID)
		if !ok || got.URL != m.URL {
			t.Errorf("ByID(%q) = %+v, %v", m.ID, got, ok)
		}
	}
	if _, ok := ByID("landing"); ok {
		t.Error("ByID(\"landing\") resolved: the landing page is a Decide surface outside the shell")
	}
}
