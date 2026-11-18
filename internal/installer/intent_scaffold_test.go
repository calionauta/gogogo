// Tests for the scaffold route's ask-first step.
//
// testAskMarker is the wording the ask step must carry. It is duplicated here
// on purpose: a test that imported the constant would keep passing if the
// sentence were emptied.
package installer

import (
	"strings"
	"testing"
)

// TestScaffoldRouteAsksBeforeApplying pins the one step an agent can skip and
// pay for: the scaffold clones a repo and rewrites a Go module path, so the
// name and owner are the operator's decision, not the model's. The step has no
// `run` — it is advice that must survive a refactor of the command lines around
// it, which is why it is asserted by position and by wording, not by the exact
// sentence.
const testAskMarker = "ASK THE"

func TestScaffoldRouteAsksBeforeApplying(t *testing.T) {
	steps := routeFor(intentScaffold, treeUnknown, "/tmp/app")
	if len(steps) < 3 {
		t.Fatalf("scaffold route has %d steps, want preview → ask → apply", len(steps))
	}
	ask := steps[1]
	if ask.Run != "" {
		t.Errorf("step 2 runs %q — the ask step is advice and must name no command", ask.Run)
	}
	for _, want := range []string{testAskMarker, "name", "owner", "languages"} {
		if !strings.Contains(ask.Why, want) {
			t.Errorf("ask step never mentions %q: %s", want, ask.Why)
		}
	}
	// The apply step must not hardcode the demo name: a copied `my-app` is the
	// failure this step exists to prevent.
	if strings.Contains(steps[2].Run, "--name my-app") {
		t.Errorf("apply step still uses the placeholder name: %q", steps[2].Run)
	}
}

// TestScaffoldRouteSurvivesTheTreeProbe pins that the ask step is text-only in
// every tree state: a route that prints a command the tree would refuse is
// worse than no route, and this step prints none.
func TestScaffoldRouteSurvivesTheTreeProbe(t *testing.T) {
	for _, tree := range []string{treeUnknown, treeCheckout, treeNotCheckout} {
		steps := routeFor(intentScaffold, tree, "/tmp/app")
		found := false
		for _, s := range steps {
			if strings.Contains(s.Why, testAskMarker) {
				found = true
				if s.Run != "" {
					t.Errorf("tree %q: ask step carries a command: %q", tree, s.Run)
				}
			}
		}
		if !found {
			t.Errorf("tree %q lost the ask-before-apply step", tree)
		}
	}
}
