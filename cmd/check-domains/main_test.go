package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/capabilities"
)

const goodCard = `# notes

**Owns.** The notes.
**Does not own.** The drawings.

## Language

| term | means | not |
|---|---|---|
| note | a document | a message |

## Invariants

- Delete requires canDeleteDoc.
`

// TestCardProblemsAcceptsACompleteCard is the green arm: the four parts, with
// content under each, is all the contract asks.
func TestCardProblemsAcceptsACompleteCard(t *testing.T) {
	if problems := cardProblems("notes", goodCard); len(problems) != 0 {
		t.Fatalf("cardProblems() = %v, want none", problems)
	}
}

// TestCardProblemsCatchesEachMissingPart proves the gate is not satisfied by
// headings: an empty "**Owns.**" or a language section with no rows fails.
func TestCardProblemsCatchesEachMissingPart(t *testing.T) {
	cases := map[string]string{
		"empty owns":      strings.Replace(goodCard, "**Owns.** The notes.", "**Owns.**", 1),
		"no does-not-own": strings.Replace(goodCard, "**Does not own.** The drawings.\n", "", 1),
		"language":        strings.Replace(goodCard, "| note | a document | a message |\n", "", 1),
		"invariants":      strings.Replace(goodCard, "- Delete requires canDeleteDoc.\n", "", 1),
	}
	for name, card := range cases {
		if problems := cardProblems("notes", card); len(problems) == 0 {
			t.Errorf("%s: cardProblems() = none, want a problem", name)
		}
	}
}

// TestCheckRequiresACardPerFeature and its reverse are separate arms: a missing
// card and a card for a slice that is gone are different drifts.
func TestCheckFlagsAMissingCard(t *testing.T) {
	root := t.TempDir()
	problems, checked := Check(root)
	if checked == 0 {
		t.Fatal("Check() checked 0 features; the registry read failed")
	}
	if len(problems) != checked {
		t.Fatalf("Check() = %d problem(s) for %d feature(s), want one per feature", len(problems), checked)
	}
	if !strings.Contains(problems[0], "missing") {
		t.Fatalf("Check() = %q, want a missing-card problem", problems[0])
	}
}

// TestCheckSkipsAUnitTheTreeDeliberatelyDropped is the regression the gate had:
// a scaffold with `--recipes none` keeps the registry entry of every dropped
// unit (a slice a kept file still imports must stay), so "no card" was reported
// for slices whose directories the scaffold had just deleted. The scaffold's
// own record is what distinguishes the two.
//
// The card is NOT written here — that is the point. The unit is listed in
// `removed-paths:` and the gate must accept its absence.
func TestCheckSkipsAUnitTheTreeDeliberatelyDropped(t *testing.T) {
	root := t.TempDir()
	// Every feature the registry knows, dropped except the one we keep. The
	// record covers exactly the directories those slices own.
	agents := "## Trim provenance\n\nremoved-paths: " +
		strings.Join(otherFeatureDirs(t), ", ") + "\n"
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(agents), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, capabilities.CardDir), 0o755); err != nil {
		t.Fatal(err)
	}

	problems, checked := Check(root)
	if len(problems) != 0 {
		t.Fatalf("Check() = %v, want none — the tree's own record says it dropped them", problems)
	}
	if checked != 0 {
		t.Fatalf("checked = %d, want 0: every feature is trimmed", checked)
	}
}

// TestCheckStillRequiresACardForASurvivingUnit is the strict arm: a record that
// covers SOME paths must not silence a unit whose directories are still here.
func TestCheckStillRequiresACardForASurvivingUnit(t *testing.T) {
	root := t.TempDir()
	agents := "## Trim provenance\n\nremoved-paths: features/unrelated\n"
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(agents), 0o600); err != nil {
		t.Fatal(err)
	}

	problems, _ := Check(root)
	if len(problems) == 0 {
		t.Fatal("a surviving feature with no card must still be reported")
	}
	if !strings.Contains(strings.Join(problems, "\n"), "missing") {
		t.Fatalf("problems = %v, want the missing-card report", problems)
	}
}

// otherFeatureDirs is every feature capability's owned directories, so the
// record in the first test is honest rather than a hand-picked list that could
// drift from the registry.
func otherFeatureDirs(t *testing.T) []string {
	t.Helper()
	var all []string
	for _, c := range capabilities.All {
		if c.Kind != capabilities.KindFeature {
			continue
		}
		all = append(all, c.Dirs...)
	}
	if len(all) == 0 {
		t.Fatal("the registry has no feature directories; this test would pass vacuously")
	}
	return all
}
