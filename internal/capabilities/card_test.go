package capabilities

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleCard = `# notes

**Owns.** The shared notes: title, body, delete rights.
**Does not own.** Presence and carets (collab), the inbox row (notifications).

## Language

| term | means | not |
|---|---|---|
| note | a saved document owned by one author | a chat message |
| share | read access granted to every signed-in user | a copy |

## Invariants

- Create stamps the owner from the session.
- Delete requires canDeleteDoc — author or admin.
`

// TestParseCardReadsTheFourParts is the green arm: the labels a card is
// written with are the labels the parser reads, and nothing between them is
// interpreted.
func TestParseCardReadsTheFourParts(t *testing.T) {
	card := ParseCard(sampleCard)
	if card.Owns != "The shared notes: title, body, delete rights." {
		t.Errorf("Owns = %q", card.Owns)
	}
	if !strings.Contains(card.DoesNotOwn, "collab") {
		t.Errorf("DoesNotOwn = %q, want the neighbour named", card.DoesNotOwn)
	}
	if len(card.Language) != 2 {
		t.Fatalf("Language = %+v, want two terms (the header row is not a term)", card.Language)
	}
	if card.Language[1] != (CardTerm{Term: "share", Means: "read access granted to every signed-in user", Not: "a copy"}) {
		t.Errorf("Language[1] = %+v", card.Language[1])
	}
	if len(card.Invariants) != 2 || !strings.Contains(card.Invariants[1], "canDeleteDoc") {
		t.Errorf("Invariants = %+v", card.Invariants)
	}
}

// TestParseCardRejectsAnUndefinedTerm pins the arm that matters for the gate:
// a row with a word and no meaning reads to an agent as a definition, so it
// must not count as one.
func TestParseCardRejectsAnUndefinedTerm(t *testing.T) {
	blank := strings.Replace(sampleCard,
		"| note | a saved document owned by one author | a chat message |",
		"| note | | |", 1)
	card := ParseCard(blank)
	if len(card.Language) != 1 || card.Language[0].Term != "share" {
		t.Fatalf("Language = %+v, want only the defined term", card.Language)
	}
}

// TestParseCardIgnoresAnEmptyLabel is the red half of "a heading is not a
// boundary": `**Owns.**` alone must parse to nothing.
func TestParseCardIgnoresAnEmptyLabel(t *testing.T) {
	headless := strings.Replace(sampleCard,
		"**Owns.** The shared notes: title, body, delete rights.", "**Owns.**", 1)
	card := ParseCard(headless)
	if card.Owns != "" {
		t.Fatalf("Owns = %q, want empty", card.Owns)
	}
}

// TestReadCardNamesTheMissingFile keeps the caller's error actionable: the
// gate prints it verbatim, so it must say which card a tree is missing.
func TestReadCardNamesTheMissingFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, CardDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CardPath(root, "notes"), []byte(sampleCard), 0o600); err != nil {
		t.Fatal(err)
	}
	card, err := ReadCard(root, "notes")
	if err != nil {
		t.Fatalf("ReadCard(notes) = %v", err)
	}
	if card.Owns == "" {
		t.Fatal("ReadCard() lost the owns line")
	}
	if _, err := ReadCard(root, "absent"); err == nil || !strings.Contains(err.Error(), CardDir+"/absent.md") {
		t.Fatalf("ReadCard(absent) = %v, want the path in the error", err)
	}
}
