package main

import (
	"strings"
	"testing"
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
