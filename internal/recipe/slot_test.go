// SCOPE:plugin — Spike 5 evidence: slots carry content without carrying identity.
//
// The kill criterion is the test: a page that renders a slot must never be in a
// position to ask which recipe contributed. These tests assert that the signature
// makes it impossible.
package recipe

import (
	"strings"
	"testing"
)

func frag(title string) Fragment {
	return Fragment{Title: title, Render: func() string { return "<div>" + title + "</div>" }}
}

// TestSlotPageRendersWithoutKnowingTheRecipe is the binary criterion: a core page
// renders contributed content through RenderSlot, whose signature takes a slot id
// and a renderer — no recipe id anywhere in it.
func TestSlotPageRendersWithoutKnowingTheRecipe(t *testing.T) {
	reg := NewSlotRegistry()
	if err := reg.Add(Contribution{
		Fragments: map[SlotID]Fragment{SlotSettingsSection: frag("Pomodoro")},
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(Contribution{
		Fragments: map[SlotID]Fragment{SlotSettingsSection: frag("Review queue")},
	}); err != nil {
		t.Fatal(err)
	}

	// This is the whole of an owning page's involvement.
	out := reg.RenderSlot(SlotSettingsSection, func(f Fragment) string {
		return "<section class=\"ui-card\">" + f.Render() + "</section>"
	})

	if !strings.Contains(out, "Pomodoro") || !strings.Contains(out, "Review queue") {
		t.Fatalf("both contributions must render, got %q", out)
	}
	if strings.Contains(out, "recipe") || strings.Contains(out, "pomodoro:") {
		t.Fatalf("no recipe identifier may leak into the page's output: %q", out)
	}
	// The page supplied the container, the recipes supplied only content.
	if strings.Count(out, "ui-card") != 2 {
		t.Fatalf("the page must own the card frame, got %q", out)
	}
}

// TestSlotRefusesASlotTheShellDoesNotOffer asserts a closed slot set: content
// offered to a slot nobody lays out would be silently dropped, which is worse than
// a registration error.
func TestSlotRefusesASlotTheShellDoesNotOffer(t *testing.T) {
	reg := NewSlotRegistry()
	err := reg.Add(Contribution{Fragments: map[SlotID]Fragment{"page.everywhere": frag("X")}})
	if err == nil {
		t.Fatal("an unknown slot must be refused, not silently ignored")
	}
	if !strings.Contains(err.Error(), "not offered") {
		t.Fatalf("the error must explain the slot set is closed, got %v", err)
	}
}

// TestSlotRefusesAShapeWithNoHostPage is the gap this gate closes: a slot can be
// declared and still have no page that draws it. `dashboard.card` has its shape
// (a cell) and no dashboard in this build, so accepting the fragment would drop
// it — and a dropped contribution is indistinguishable from a feature nobody
// built, which is the failure mode the slot contract exists to prevent.
func TestSlotRefusesAShapeWithNoHostPage(t *testing.T) {
	reg := NewSlotRegistry()
	err := reg.Add(Contribution{Fragments: map[SlotID]Fragment{SlotDashboardCard: frag("X")}})
	if err == nil {
		t.Fatal("a slot with no host page must be refused, not accepted and dropped")
	}
	if !strings.Contains(err.Error(), "not hosted") {
		t.Fatalf("the error must say the slot has no host, got %v", err)
	}
	if got := reg.Fragments(SlotDashboardCard); len(got) != 0 {
		t.Fatalf("a refused contribution must not reach the registry, got %v", got)
	}
}

// TestEveryHostedSlotIsOffered pins the two tables together: a host without an
// offer would be a page rendering a slot nobody may fill, and an offer without a
// host is the silent drop above. `dashboard.card` is asserted to have no host on
// purpose — the day a dashboard page exists, this test is where the offer flips.
func TestEveryHostedSlotIsOffered(t *testing.T) {
	for _, slot := range []SlotID{SlotSettingsSection, SlotNavSidebar, SlotRecordRowActions} {
		host, ok := HostFor(slot)
		if !ok || host == "" {
			t.Errorf("slot %q must name the page that renders it", slot)
		}
		if !Offered(slot) {
			t.Errorf("slot %q has a host, so it must be offered", slot)
		}
	}
	if host, ok := HostFor(SlotDashboardCard); ok {
		t.Fatalf("dashboard.card reports host %q: this build has no dashboard page, so it must stay unoffered", host)
	}
	if Offered(SlotDashboardCard) {
		t.Fatal("dashboard.card must not be offered while no page renders it")
	}
}

// TestSlotShapeIsDeclared asserts every offered slot has a container contract. A
// slot without one means "somewhere in this page", which is the layout rot the
// research section warns about.
func TestSlotShapeIsDeclared(t *testing.T) {
	for _, slot := range []SlotID{SlotDashboardCard, SlotSettingsSection, SlotNavSidebar, SlotRecordRowActions} {
		shape, ok := ShapeFor(slot)
		if !ok {
			t.Errorf("slot %q has no declared shape", slot)
			continue
		}
		if shape == "" {
			t.Errorf("slot %q has an empty shape", slot)
		}
	}
	if _, ok := ShapeFor("invented.slot"); ok {
		t.Error("an unknown slot must not resolve to a shape")
	}
}

// TestRowActionsAreDataNotComponents is the rule that keeps a recipe out of a core
// handler: actions on another recipe's entity are declared as data, so the owning
// page decides how to present and authorize them.
func TestRowActionsAreDataNotComponents(t *testing.T) {
	reg := NewSlotRegistry()
	if err := reg.Add(Contribution{
		Actions: map[SlotID][]Action{SlotRecordRowActions: {
			{Label: "Start timer", Href: "/pomodoro/start/{id}", Verb: "post", Confirm: "Start a timer for this task?"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	got := reg.Actions(SlotRecordRowActions)
	if len(got) != 1 {
		t.Fatalf("expected one action, got %v", got)
	}
	if got[0].Verb != "post" || got[0].Confirm == "" {
		t.Fatalf("an action must carry enough for the owning page to render it safely: %+v", got[0])
	}
	// The owning page renders it; the recipe supplied no templ and no handler
	// reference, which is what keeps the core handler untouched.
	if shape, _ := ShapeFor(SlotRecordRowActions); shape != ShapeActions {
		t.Fatalf("row actions must be a declarative shape, got %s", shape)
	}
}

// TestSlotRenderIsStable asserts contributions render in a stable order, so a page
// does not reshuffle between requests.
func TestSlotRenderIsStable(t *testing.T) {
	reg := NewSlotRegistry()
	for _, title := range []string{"zeta", "alpha", "mid"} {
		if err := reg.Add(Contribution{Fragments: map[SlotID]Fragment{SlotSettingsSection: frag(title)}}); err != nil {
			t.Fatal(err)
		}
	}
	out := reg.RenderSlot(SlotSettingsSection, func(f Fragment) string { return f.Title + "|" })
	if out != "alpha|mid|zeta|" {
		t.Fatalf("expected stable title order, got %q", out)
	}
}

// TestEmptySlotRendersNothing asserts a page with no contributions is valid: a
// project that installs nothing must not gain empty chrome.
func TestEmptySlotRendersNothing(t *testing.T) {
	reg := NewSlotRegistry()
	if out := reg.RenderSlot(SlotSettingsSection, func(Fragment) string { return "X" }); out != "" {
		t.Fatalf("an empty slot must render nothing, got %q", out)
	}
}

// TestEntryMustBeARootRelativePath pins the rail's security invariant at
// registration, not at render: an extension destination is a path this binary
// serves. A scheme (`javascript:`), an absolute URL and a protocol-relative
// `//host` are all refused, so a recipe cannot turn the sidebar into a redirect.
func TestEntryMustBeARootRelativePath(t *testing.T) {
	for _, href := range []string{"https://evil.example/x", "javascript:alert(1)", "//evil.example/x", "pomodoro"} {
		reg := NewSlotRegistry()
		err := reg.Add(Contribution{Entries: map[SlotID][]Entry{
			SlotNavSidebar: {{Label: "Evil", Href: href}},
		}})
		if err == nil {
			t.Errorf("href %q was accepted; the rail would leave the origin", href)
		}
	}

	reg := NewSlotRegistry()
	if err := reg.Add(Contribution{Entries: map[SlotID][]Entry{
		SlotNavSidebar: {{Label: "Pomodoro", Href: "/pomodoro"}},
	}}); err != nil {
		t.Fatalf("a root-relative path must be accepted, got %v", err)
	}
	if got := reg.Entries(SlotNavSidebar); len(got) != 1 || got[0].Href != "/pomodoro" {
		t.Fatalf("entry did not reach the registry: %+v", got)
	}
}

// TestRowActionContract pins what a row action may be: a label, a closed verb,
// a root-relative template naming the row exactly once. Together those are the
// difference between "a recipe offered an action on my row" and "a recipe
// redirected my browser", and none of them is a rendering concern.
func TestRowActionContract(t *testing.T) {
	bad := []Action{
		{Label: "", Verb: "post", Href: "/api/x/{id}"},
		{Label: "Go", Verb: "delete", Href: "/api/x/{id}"},
		{Label: "Go", Verb: "post", Href: "https://evil.example/x/{id}"},
		{Label: "Go", Verb: "post", Href: "/api/x"},           // no row named
		{Label: "Go", Verb: "post", Href: "/api/x/{id}/{id}"}, // ambiguous row
	}
	for _, a := range bad {
		reg := NewSlotRegistry()
		err := reg.Add(Contribution{Actions: map[SlotID][]Action{SlotRecordRowActions: {a}}})
		if err == nil {
			t.Errorf("action %+v was accepted", a)
		}
	}

	reg := NewSlotRegistry()
	ok := Action{Label: "Send to pomodoro", Verb: "post", Href: "/api/pomodoro/from-todo/{id}", Confirm: "Send this row?"}
	if err := reg.Add(Contribution{Actions: map[SlotID][]Action{SlotRecordRowActions: {ok}}}); err != nil {
		t.Fatalf("a well-formed action must be accepted, got %v", err)
	}
	if got := reg.Actions(SlotRecordRowActions); len(got) != 1 || got[0].Href != ok.Href {
		t.Fatalf("action did not reach the registry: %+v", got)
	}
}
