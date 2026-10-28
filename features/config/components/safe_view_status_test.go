// SCOPE:feature — the /config status card derives from the runtime config.
package components

import (
	"testing"

	"github.com/calionauta/gogogo/config"
)

// TestStatusSummaryFlagsDevAndMissingKeys pins the card's contract: words,
// not bools, and a missing key reads as missing — never as a green badge.
// Revert a tone and the operator misreads the posture at a glance.
func TestStatusSummaryFlagsDevAndMissingKeys(t *testing.T) {
	t.Parallel()
	cfg := config.Load()
	cfg.Dev = true
	cfg.RBACEnabled = false
	cfg.GoAI.APIKey = ""
	states := map[string]StatusItem{}
	for _, item := range StatusSummary(cfg, false, true) {
		states[item.Label] = item
	}
	for label, want := range map[string]string{
		"Environment": "development",
		"RBAC":        "pass-through",
		"AI key":      "missing",
		"Secrets key": "missing",
	} {
		if states[label].State != want {
			t.Errorf("%s state = %q, want %q", label, states[label].State, want)
		}
		if states[label].Tone == "success" {
			t.Errorf("%s tone is success for a %q state", label, want)
		}
		if states[label].Hint == "" {
			t.Errorf("%s carries no hint", label)
		}
		if states[label].Group == "" {
			t.Errorf("%s carries no group", label)
		}
	}
}

// TestStatusSummaryKeepsUnusedAgeKeyNeutral is the honesty fix: a deployment
// that never used age secrets reported a red MISSING, which reads as a
// broken deploy. No file and no key is "not in use" (neutral); a file whose
// key is gone stays an error. Revert the switch and the neutral assertion
// goes red.
func TestStatusSummaryKeepsUnusedAgeKeyNeutral(t *testing.T) {
	t.Parallel()
	cfg := config.Load()
	unused := statusItem(StatusSummary(cfg, false, false), "Secrets key")
	if unused.State != "not in use" || unused.Tone == toneError {
		t.Errorf("unused age store = %+v, want neutral \"not in use\"", unused)
	}
	broken := statusItem(StatusSummary(cfg, false, true), "Secrets key")
	if broken.State != "missing" || broken.Tone != toneError {
		t.Errorf("configured age store without key = %+v, want error \"missing\"", broken)
	}
	if set := statusItem(StatusSummary(cfg, true, true), "Secrets key"); set.State != "set" || set.Tone != toneSuccess {
		t.Errorf("keyed age store = %+v, want success \"set\"", set)
	}
}

// TestStatusGroupsKeepPinnedOrder pins the grouping: every item lands under
// one of the declared captions, and the columns render in reading order.
// Ungrouped items would silently vanish from the card (groupStatus drops a
// group with no rows), which is the failure a "nice" refactor ships.
func TestStatusGroupsKeepPinnedOrder(t *testing.T) {
	t.Parallel()
	groups := groupStatus(StatusSummary(config.Load(), true, true))
	if len(groups) != len(statusGroupOrder) {
		t.Fatalf("groups = %+v, want %d captions", groups, len(statusGroupOrder))
	}
	for i, group := range groups {
		if group.Name != statusGroupOrder[i] {
			t.Errorf("group %d = %q, want %q", i, group.Name, statusGroupOrder[i])
		}
		if len(group.Items) == 0 {
			t.Errorf("group %q rendered empty", group.Name)
		}
	}
	if len(groupStatus(nil)) != 0 {
		t.Error("no items must render no columns")
	}
}

// TestStatusAttentionOnlyCountsWarnings pins the summary badge: postures
// (info/neutral) are not problems. A keyless demo judge must not read as
// "needs attention", or the badge cries wolf on every default install.
func TestStatusAttentionOnlyCountsWarnings(t *testing.T) {
	t.Parallel()
	items := []StatusItem{
		{Tone: toneSuccess},
		{Tone: toneInfo},
		{Tone: toneNeutral},
		{Tone: toneWarning},
		{Tone: toneError},
	}
	if got := StatusAttention(items); got != 2 {
		t.Errorf("StatusAttention = %d, want 2 (warning + error)", got)
	}
	if got := AttentionLabel(1); got != "1 needs attention" {
		t.Errorf("AttentionLabel(1) = %q", got)
	}
	if got := AttentionLabel(2); got != "2 need attention" {
		t.Errorf("AttentionLabel(2) = %q", got)
	}
}

// TestBuildPageDataWiresStatus pins the card: PageData carries both the flat
// rows and their grouped view, so the template is never a heading over
// nothing. Genui-free by construction (the card derives from the runtime
// config, not the answer catalog), so this file survives the genui trim.
func TestBuildPageDataWiresStatus(t *testing.T) {
	t.Parallel()
	pd := BuildPageData(config.Load())
	if len(pd.Status) == 0 {
		t.Error("PageData carries no status rows")
	}
	if len(pd.StatusGroups) == 0 {
		t.Error("PageData carries no grouped status")
	}
}

// statusItem finds one row by label, or fails the test.
func statusItem(items []StatusItem, label string) StatusItem {
	for _, item := range items {
		if item.Label == label {
			return item
		}
	}
	return StatusItem{}
}
