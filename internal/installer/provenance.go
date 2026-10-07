// SCOPE:layer=infra,removal=plugin — installer engine: trim-provenance reader
package installer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// trimProvenance records which units a scaffolded project deliberately removed.
//
// WHY THIS EXISTS: `--check` answers "would applyTrim succeed fully here?", so
// against a deliberately trimmed tree every marker it looks for is legitimately
// absent — and it reported all of them as CHECK-FAIL with exit 1. A human
// reading that knows to ignore it, but an LLM agent reads exit 1 + the word
// FAIL and concludes the scaffold is broken. That is precisely the wrong
// conclusion, and it is a communication bug rather than a correctness one: the
// tree is correct, the verdict is misleading.
//
// The information needed to tell the two apart already exists — `apply` writes
// a Trim provenance section into the generated AGENTS.md — so this reads it
// instead of asking the user to remember, and instead of guessing.
type trimProvenance struct {
	// found is false when there is no AGENTS.md, no provenance section, or no
	// parseable list. Callers must then fall back to strict behaviour rather
	// than assuming "nothing was trimmed".
	found bool
	// removed is the set of unit ids recorded as removed at scaffold time.
	removed map[string]bool
}

// removedUnitsRE captures the unit ids from the provenance line the installer
// writes. Kept deliberately narrow: it matches only ids the manifest actually
// knows, so a future prose change cannot silently turn documentation into a
// verdict.
var provenanceIDsRE = regexp.MustCompile(`(?m)^removed:\s*(.+)$`)

// readTrimProvenance reads the recorded trim from a scaffolded checkout.
//
// A missing or unparseable section yields found=false, which makes `--check`
// behave exactly as it did before — strict. That direction is deliberate: the
// risk of a false "this is fine" is worse than a noisy failure, because the
// whole point of the gate is to catch drift.
func readTrimProvenance(root string) trimProvenance {
	tp := trimProvenance{removed: map[string]bool{}}
	raw, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		return tp
	}
	text := string(raw)
	if !strings.Contains(text, "## Trim provenance") {
		return tp
	}
	m := provenanceIDsRE.FindStringSubmatch(text)
	if m == nil {
		return tp
	}
	known := map[string]bool{}
	for _, u := range manifestUnits {
		known[u.id] = true
	}
	for id := range strings.SplitSeq(m[1], ",") {
		id = strings.TrimSpace(id)
		// "none" is the installer's marker for an empty trim, written so the
		// line is always present and parseable.
		if id == "none" || id == "" {
			continue
		}
		// Only ids the manifest knows are trusted. Anything else is prose
		// drift, not a verdict.
		if known[id] {
			tp.removed[id] = true
		}
	}
	// The LINE is what establishes provenance, not having removed anything: a
	// full template with "removed: none" must still be trusted (every unit
	// legitimately applies, so strict and lenient agree there anyway), and
	// requiring a non-empty set made that case look like missing provenance.
	tp.found = true
	return tp
}

// explainTrim rewrites the check result for a deliberately trimmed tree.
//
// Returns the number of units that are still genuinely broken — i.e. units that
// were NOT recorded as removed and yet do not apply. A unit recorded as removed
// is reported as CHECK-TRIMMED and does not count as a failure, so the exit
// code stops conflating "you asked for this" with "something is wrong".
func explainTrim(w io.Writer, results []unitCheck, tp trimProvenance) int {
	if !tp.found {
		return -1 // caller keeps the strict path
	}
	failed := 0
	for _, uc := range results {
		if len(uc.Problems) == 0 {
			fmt.Fprintf(w, "CHECK-OK %s\n", uc.ID)
			continue
		}
		if tp.removed[uc.ID] {
			fmt.Fprintf(w, "CHECK-TRIMMED %s (recorded as removed at scaffold time)\n", uc.ID)
			continue
		}
		// Not recorded as removed, yet it does not apply — this is the real
		// signal the gate exists for.
		failed++
		for _, p := range uc.Problems {
			fmt.Fprintf(w, "CHECK-FAIL %s %s\n", uc.ID, p)
		}
	}
	return failed
}

// removedIDs extracts the unit ids from a trim plan, sorted for a stable
// AGENTS.md (a shuffled list would show as a diff on every scaffold).
func removedIDs(drop []trimUnit) []string {
	ids := make([]string, 0, len(drop))
	for _, u := range drop {
		ids = append(ids, u.id)
	}
	sort.Strings(ids)
	return ids
}
