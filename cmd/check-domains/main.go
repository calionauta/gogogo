// Command check-domains is the gate for the per-slice domain cards.
//
// A capability card (`docs/domain/<id>.md`) is the slice's written boundary:
// what it owns, what it deliberately does not, the words it uses, and the rules
// its code must keep. It exists because the expensive failure is not missing
// code — it is a plausible parallel model of the domain: an invented field, a
// second owner column, a delete that skips the policy, a synonym for a noun
// that already has a name.
//
// The gate is mechanical and text-only. For every feature capability it
// requires a card with four non-empty parts (owns, does not own, language table,
// invariants list); for every card it requires a feature that owns it. It does
// not judge the prose — review does that — because a gate that guessed at
// meaning would be the second source of truth the cards exist to prevent.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/calionauta/gogogo/internal/capabilities"
)

// dir is where the cards live, relative to the tree root. The parser is
// shared with the MCP catalog (`capabilities_list`), which reports the same
// boundary to a client that never sees this gate.
var dir = capabilities.CardDir

func main() {
	root := flag.String("dir", ".", "tree to check")
	quiet := flag.Bool("quiet", false, "print only failures")
	flag.Parse()

	problems, checked := Check(*root)
	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, p)
		}
		fmt.Fprintf(os.Stderr, "check-domains: %d problem(s); a feature's card lives in %s/<id>.md\n",
			len(problems), dir)
		os.Exit(1)
	}
	if !*quiet {
		fmt.Printf("check-domains: %d feature card(s) present and complete\n", checked)
	}
}

// Check reports the cards' problems and how many it verified. Split from main
// so a test can drive it against a temp tree.
func Check(root string) ([]string, int) {
	var problems []string
	features := map[string]bool{}
	for _, c := range capabilities.All {
		if c.Kind != capabilities.KindFeature {
			continue
		}
		features[c.ID] = true
		raw, err := os.ReadFile(capabilities.CardPath(root, c.ID))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s/%s.md: missing (%s is a feature unit; "+
				"write owns / does-not-own / language / invariants)", dir, c.ID, c.ID))
			continue
		}
		problems = append(problems, cardProblems(c.ID, string(raw))...)
	}

	// The reverse direction: a card for a feature that no longer exists is the
	// stale half of the same drift.
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		problems = append(problems, fmt.Sprintf("%s: %v", dir, err))
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".md")
		if !features[id] {
			problems = append(problems, fmt.Sprintf("%s/%s: no feature unit named %q "+
				"(a card with no slice is the boundary nobody enforces)", dir, e.Name(), id))
		}
	}
	sort.Strings(problems)
	return problems, len(features)
}

// cardProblems validates one card's structure. The four parts are the contract:
// a card missing one is a card an agent would read and still have to guess.
// The check runs on the parsed card rather than on the raw text, so a heading
// with nothing under it cannot pass as a boundary.
func cardProblems(id, text string) []string {
	card := capabilities.ParseCard(text)
	var problems []string
	if card.Owns == "" {
		problems = append(problems, id+": missing a filled `**Owns.**` line (what this slice is responsible for)")
	}
	if card.DoesNotOwn == "" {
		problems = append(problems, id+": missing a filled `**Does not own.**` line (the nearest wrong neighbour)")
	}
	if len(card.Language) == 0 {
		problems = append(problems, id+": `## Language` needs a table with at least one defined term")
	}
	if len(card.Invariants) == 0 {
		problems = append(problems, id+": `## Invariants` needs at least one bullet")
	}
	return problems
}
