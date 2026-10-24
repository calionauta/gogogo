// SCOPE:feature — the /config status strip derives from the runtime config.
package components

import (
	"testing"

	"github.com/calionauta/gogogo/config"
)

// TestStatusSummaryFlagsDevAndMissingKeys pins the strip's contract: words,
// not bools, and a missing key reads as missing — never as a green badge.
// Revert a tone and the operator misreads the posture at a glance.
func TestStatusSummaryFlagsDevAndMissingKeys(t *testing.T) {
	t.Parallel()
	cfg := config.Load()
	cfg.Dev = true
	cfg.RBACEnabled = false
	cfg.GoAI.APIKey = ""
	states := map[string]StatusItem{}
	for _, item := range StatusSummary(cfg, false) {
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
	}
}

// TestBuildPageDataWiresStatus pins the strip: PageData carries it, so the
// status strip is never a heading over nothing. Genui-free by construction
// (the strip derives from the runtime config, not the answer catalog), so
// this file survives the genui trim.
func TestBuildPageDataWiresStatus(t *testing.T) {
	t.Parallel()
	pd := BuildPageData(config.Load())
	if len(pd.Status) == 0 {
		t.Error("PageData carries no status strip")
	}
}
