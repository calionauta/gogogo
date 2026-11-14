// Domain cards: the written boundary of one feature slice.
//
// docs/domain/<id>.md is a slice's bounded context — what it owns, what it
// deliberately does not, the words it uses and the rules its code must keep.
// The parser lives next to the registry because a card is a capability's
// metadata and both readers (`cmd/check-domains`, the MCP `capabilities_list`)
// already import this package; a second parser in either place would be the
// drift the card itself exists to prevent.
package capabilities

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CardDir is where the cards live, relative to a tree root.
const CardDir = "docs/domain"

// CardTerm is one row of a card's language table: the word, what it means
// here, and (when the card says so) the nearest thing it is not.
type CardTerm struct {
	Term  string
	Means string
	Not   string
}

// Card is one domain card, parsed. Empty fields are what the gate reports:
// a heading with nothing under it is not a boundary.
type Card struct {
	Owns       string
	DoesNotOwn string
	Language   []CardTerm
	Invariants []string
}

// CardPath is the card's path inside root.
func CardPath(root, id string) string {
	return filepath.Join(root, filepath.FromSlash(CardDir), id+".md")
}

// ReadCard reads and parses the card for id. The error names the file, so a
// caller reporting "no card" does not have to reconstruct the path.
func ReadCard(root, id string) (Card, error) {
	raw, err := os.ReadFile(CardPath(root, id))
	if err != nil {
		return Card{}, fmt.Errorf("%s/%s.md: %w", CardDir, id, err)
	}
	return ParseCard(string(raw)), nil
}

// ParseCard splits a card into its four parts. It is deliberately literal: it
// reads the labels a card is written with, and never infers meaning from the
// prose between them.
func ParseCard(text string) Card {
	var card Card
	section := ""
	for line := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "**Owns.**"):
			if card.Owns == "" {
				card.Owns = strings.TrimSpace(strings.TrimPrefix(trimmed, "**Owns.**"))
			}
		case strings.HasPrefix(trimmed, "**Does not own.**"):
			if card.DoesNotOwn == "" {
				card.DoesNotOwn = strings.TrimSpace(strings.TrimPrefix(trimmed, "**Does not own.**"))
			}
		case strings.HasPrefix(trimmed, "#"):
			section = strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		case section == "Language" && strings.HasPrefix(trimmed, "|"):
			if term, ok := tableRow(trimmed); ok {
				card.Language = append(card.Language, term)
			}
		case section == "Invariants" && strings.HasPrefix(trimmed, "- "):
			card.Invariants = append(card.Invariants, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
		}
	}
	return card
}

// tableRow parses one markdown table line. The header row (its first cell
// names the column) and the `---` separator are not terms.
func tableRow(line string) (CardTerm, bool) {
	cells := strings.Split(strings.Trim(line, "|"), "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	if len(cells) == 0 || cells[0] == "" || cells[0] == "term" || strings.Contains(cells[0], "---") {
		return CardTerm{}, false
	}
	term := CardTerm{Term: cells[0]}
	if len(cells) > 1 {
		term.Means = cells[1]
	}
	if len(cells) > 2 {
		term.Not = cells[2]
	}
	// A row whose meaning is blank carries a word and no definition, which is
	// worse than no row: it reads as defined.
	return term, term.Means != ""
}
