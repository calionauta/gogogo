// SCOPE:plugin — prompt rules for the app's own data (red-proof).
//
// Two reported failures are pinned here, both from the same root: the prompt
// treated the app's panels as one option among several, so the model answered
// questions about the user's own data by inventing it.
//
//  1. "plan my days from to-dos" came back as a data_table of made-up todo
//     titles because nothing forbade fabricating them.
//  2. A panel whose hydration failed was dropped silently, so the answer had a
//     hole where the only useful part was.
package components

import (
	"strings"
	"testing"
)

// TestPanelsPromptForbidsInventingUserData is the red-proof for the fabrication
// report. Soften the prohibition back into a suggestion ("reach for ... when the
// request is about it") and the "NEVER invent" assertion goes red.
func TestPanelsPromptForbidsInventingUserData(t *testing.T) {
	got := PromptForChat()
	if !strings.Contains(got, "NEVER invent the user's own data") {
		t.Errorf("the catalog prompt must forbid inventing user data:\n%s", got)
	}
	if !strings.Contains(got, "top-level component") {
		t.Errorf("the prompt must name the required answer shape:\n%s", got)
	}
}

// TestDataRuleLandsLast pins the placement, which is the point: the reminder
// has to sit immediately before the request, because a rule buried mid-prompt
// is what a long vocabulary beats into background. Red-proof: drop dataRule
// from promptForSurfaces and the trailing-reminder assertion goes red.
func TestDataRuleLandsLast(t *testing.T) {
	got := PromptForChat()
	reminder := strings.Index(got, "Reminder: the user's own data is reachable ONLY")
	noProse := strings.Index(got, "No prose outside the JSON object.")
	if reminder < 0 {
		t.Fatalf("no trailing data reminder in the prompt:\n%s", got)
	}
	if reminder > noProse {
		t.Errorf("the reminder must precede the closing rule (reminder at %d, closing at %d):\n%s",
			reminder, noProse, got)
	}
}

// TestPanelsPromptAppearsOnSurfaces pins that the rule reaches every surface
// set that advertises panels: the failure was invented user data. A set
// without native advertises no panels, so there is nothing to forbid.
func TestPanelsPromptAppearsOnSurfaces(t *testing.T) {
	for name, prompt := range map[string]string{
		"catalog+native": PromptForSurfaces([]Surface{SurfaceCatalog, SurfaceNative}),
		"chat":           PromptForChat(),
	} {
		if !strings.Contains(prompt, "NEVER invent the user's own data") {
			t.Errorf("%s prompt is missing the data rule:\n%s", name, prompt)
		}
	}
	if got := PromptForSurfaces([]Surface{SurfaceCatalog}); strings.Contains(got, "NEVER invent") {
		t.Errorf("a set with no panels carries the panel rule:\n%s", got)
	}
}

// TestResolvePanelName_Aliases pins the spelling fix that stopped a correct
// answer from being thrown away: models write TODO_LIST's plural/synonyms, and
// rejecting them cost the user the whole panel.
func TestResolvePanelName_Aliases(t *testing.T) {
	for _, alias := range []string{"todos", "todo", "TODOS", " task_list ", "tasks"} {
		if got := ResolvePanelName(alias); got != "todo_list" {
			t.Errorf("ResolvePanelName(%q) = %q, want todo_list", alias, got)
		}
	}
	// A non-alias is returned unchanged, and an unknown name still resolves to
	// itself so the fail-closed path downstream is untouched.
	if got := ResolvePanelName("bar_chart"); got != "bar_chart" {
		t.Errorf("non-alias changed: %q", got)
	}
	if got := ResolvePanelName("invented_thing"); got != "invented_thing" {
		t.Errorf("unknown name changed: %q", got)
	}
	if IsPanel("todos") == false {
		t.Error("an alias must report as an installed panel")
	}
	if IsPanel("invented_thing") {
		t.Error("an unknown name must not report as a panel")
	}
}

// TestSmalltalkRuleIsPanelGated pins the greeting guard next to the rule it
// constrains: with panels advertised, greetings are ordered to never emit
// one; without panels there is nothing to guard, so the rule stays out of
// the prompt instead of naming components the set cannot render.
func TestSmalltalkRuleIsPanelGated(t *testing.T) {
	withPanels := PromptForSurfaces([]Surface{SurfaceCatalog, SurfaceNative})
	if !strings.Contains(withPanels, "never emit a panel or a data component") {
		t.Errorf("panelled prompt lost the smalltalk guard:\n%s", withPanels)
	}
	for name, prompt := range map[string]string{
		"catalog-only": PromptForSurfaces([]Surface{SurfaceCatalog}),
		"prose-only":   PromptForSurfaces([]Surface{}),
		"custom-only":  PromptForSurfaces([]Surface{SurfaceCustom}),
	} {
		if strings.Contains(prompt, "never emit a panel") {
			t.Errorf("%s prompt carries a panel rule with no panels:\n%s", name, prompt)
		}
		if strings.Contains(prompt, "text_note") && name == "custom-only" {
			t.Errorf("%s prompt names a component it cannot render:\n%s", name, prompt)
		}
	}
}
