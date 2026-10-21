// SCOPE:plugin — installer engine: trim-provenance reader
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
		if id == keepNone || id == "" {
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

// removedPaths is the machine-read companion to removedIDs: the union of
// repo-relative dirs and files the dropped units owned, sorted for a stable
// AGENTS.md. The registry conformance tests skip exactly these paths, so a
// deliberately trimmed tree does not report its own removals as missing.
func removedPaths(drop []trimUnit) string {
	seen := map[string]bool{}
	var out []string
	for _, u := range drop {
		m := u.meta()
		for _, p := range append(append([]string{}, m.dirs...), m.files...) {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return keepNone
	}
	return strings.Join(out, ", ")
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

// --- template identity (the BASE marker) ---
//
// WHY THIS EXISTS. Everything the installer can say about "is this project
// current?" needs one datum it did not record: WHICH template revision the
// tree came from. Without it, a copied project can only be compared by
// eyeballing files, and `gogogo --check-update` could only ever compare the
// BINARY against the latest release — which reports a three-month-old tree as
// current the moment you update the binary.
//
// This is the same datum every scaffolder that supports updates records:
// copier's .copier-answers.yml ref, cruft's .cruft.json commit, agent-native's
// refs/agent-native/template-baseline. It lives in AGENTS.md because that file
// is already the machine-read provenance record this package parses (`removed:`,
// `removed-paths:`), and because a project that deletes its AGENTS.md has
// opted out of agent-assistance anyway.

// templateMarker is the recorded base: which repo and revision this tree was
// scaffolded from. Found is false for a tree that predates the marker, which
// callers must treat as unknown — never as "up to date".
type templateMarker struct {
	Ref    string // "owner/repo@tag", or "owner/repo@dev" for a build without a tag
	Commit string // short commit at scaffold time, "unknown" when it was not a git checkout
	Found  bool
}

// markerUnknown is what the installer writes when it cannot resolve a
// revision (a tree that is not a git checkout). A value, not a missing line:
// the marker stays parseable and says exactly what it knows.
const markerUnknown = "unknown"

// shortCommitLen is the git short-SHA length everyone recognizes.
const shortCommitLen = 7

var (
	// scaffoldedFromRE matches the ref line. Narrow on purpose, like
	// provenanceIDsRE: a prose change must not become a verdict.
	scaffoldedFromRE = regexp.MustCompile(`(?m)^scaffolded-from:[ \t]*(\S+)[ \t]*$`)
	// templateCommitRE matches a short or full hex SHA, or the literal
	// "unknown" the installer writes for a non-git checkout.
	templateCommitRE = regexp.MustCompile(`(?m)^template-commit:[ \t]*([0-9a-fA-F]{7,40}|unknown)[ \t]*$`)
)

// readTemplateMarker reads the recorded base from a scaffolded checkout.
//
// A missing marker is not an error: trees scaffolded before this existed are
// normal, and guessing "probably latest" for them would turn the one honest
// answer ("I cannot tell") into a wrong one.
func readTemplateMarker(root string) templateMarker {
	raw, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		return templateMarker{}
	}
	text := string(raw)
	ref := scaffoldedFromRE.FindStringSubmatch(text)
	if ref == nil {
		return templateMarker{}
	}
	m := templateMarker{Ref: ref[1], Commit: markerUnknown, Found: true}
	if commit := templateCommitRE.FindStringSubmatch(text); commit != nil {
		m.Commit = commit[1]
	}
	return m
}

// shortCommit is the 7-character form for display; anything shorter or the
// literal "unknown" passes through.
func shortCommit(commit string) string {
	if len(commit) > shortCommitLen {
		return commit[:shortCommitLen]
	}
	return commit
}
