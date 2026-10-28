// SCOPE:core — Shell registry contract.
package ui

import "testing"

// adminOnlyIDs are the modules whose ROUTES are admin-only in the RBAC policy
// (internal/auth policy + the admin gate on /config). Kept explicit, not read
// back from the Module.Admin flag: a test that consults the same field the
// sidebar filters by cannot notice the flag being dropped — it was written that
// way first, and red-proofing showed it stayed green with `Admin: true` deleted.
var adminOnlyIDs = map[string]bool{"config": true}

// TestGroupedHidesAdminModulesFromNonAdmins pins the role rule the rail has: an
// admin-only module never renders for a viewer or a plain user. Config is
// MenuOnly (its entry point is the account menu), so the rail check is the
// first half; TestMenuOnlyModulesAreAccountOnly pins the second.
func TestGroupedHidesAdminModulesFromNonAdmins(t *testing.T) {
	for _, role := range []string{"", "user", "viewer"} {
		for _, g := range Grouped(role) {
			for _, m := range g.Modules {
				if adminOnlyIDs[m.ID] {
					t.Errorf("role %q sees admin-only module %q in the rail", role, m.ID)
				}
			}
		}
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

// TestMenuOnlyModulesAreAccountOnly pins the split this revision introduced:
// Config left the rail and lives in the account menu, so it must never render
// as a rail item, and it must stay an admin route (the menu guards the link
// with `userRole == "admin"` — a non-admin route there would 403 on click).
func TestMenuOnlyModulesAreAccountOnly(t *testing.T) {
	for _, role := range []string{"", "user", "viewer", "admin"} {
		for _, g := range Grouped(role) {
			for _, m := range g.Modules {
				if m.MenuOnly {
					t.Errorf("role %q sees menu-only module %q in the rail", role, m.ID)
				}
			}
		}
	}
	seen := 0
	for _, m := range Modules {
		if !m.MenuOnly {
			continue
		}
		seen++
		if !m.Admin || !adminOnlyIDs[m.ID] {
			t.Errorf("menu-only module %q must be an admin route: "+
				"the account menu guards it with userRole == admin", m.ID)
		}
	}
	if seen == 0 {
		t.Fatal("no MenuOnly module: Config is meant to live in the account menu, not the rail")
	}
}

// TestGroupedPinsInboxFirst pins the rail order the product asked for: the
// inbox is the first thing, above the Collab heading and without a heading of
// its own. It is the rail's always-on surface, not a member of a themed block.
func TestGroupedPinsInboxFirst(t *testing.T) {
	groups := Grouped("user")
	if len(groups) < 2 {
		t.Fatalf("grouped user modules = %d, want a pinned block plus Collab", len(groups))
	}
	if groups[0].Label != "" {
		t.Errorf("first group label = %q, want \"\" (the pinned block renders no heading)", groups[0].Label)
	}
	if len(groups[0].Modules) != 1 || groups[0].Modules[0].ID != ModuleInbox {
		t.Errorf("pinned block = %+v, want exactly the inbox", groups[0].Modules)
	}
	if groups[1].Label != GroupCollab {
		t.Errorf("second group = %q, want %q", groups[1].Label, GroupCollab)
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
	for _, banned := range []string{"chat", "review", "room", "config", ModuleInbox} {
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
	for _, want := range []string{"todo", "whiteboard", "notes", "chat", "review", "room", ModuleInbox} {
		if !userIDs[want] {
			t.Errorf("user does not see %q", want)
		}
	}
}

// TestGroupedKeepsEveryModuleVisibleSomewhere fails when a module is added to
// the registry without a way to reach it — the module would exist as a route
// with no entry point (a dropped scope, not a layout choice). A rail group may
// be unlabelled only when it is the leading pinned block; a MenuOnly module is
// reached from the account menu instead of the rail.
func TestGroupedKeepsEveryModuleVisibleSomewhere(t *testing.T) {
	rail := map[string]bool{}
	for i, g := range Grouped("admin") {
		if g.Label == "" {
			if i != 0 {
				t.Errorf("unlabelled group at position %d: only the pinned block may omit its heading", i)
			}
			for _, m := range g.Modules {
				if !m.Pinned {
					t.Errorf("module %q rendered in the unlabelled block without Pinned", m.ID)
				}
			}
		}
		for _, m := range g.Modules {
			rail[m.ID] = true
			if m.URL == "" || m.Title == "" || m.Icon == "" {
				t.Errorf("module %q is missing URL/Title/Icon", m.ID)
			}
		}
	}
	for _, m := range Modules {
		switch {
		case m.MenuOnly:
			if rail[m.ID] {
				t.Errorf("menu-only module %q is also rendered in the rail", m.ID)
			}
		case !rail[m.ID]:
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
