// Spike 0 probe: measure the anchor model's failure modes.
//
// This file answers one question with evidence rather than argument: when the
// core moves, does `add` fail loudly, fail silently, or insert in the wrong
// place? It is a spike artifact — it asserts the CURRENT behavior (including
// the bad parts) so the finding is recorded in a test rather than in prose that
// can drift.
package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSpikeAnchorDeadFailsLoudly records the good failure mode: with the anchor
// line absent, insertSpan cannot find it and refuses. This is what makes the
// single-template model safe today.
func TestSpikeAnchorDeadFailsLoudly(t *testing.T) {
	dst := strings.Split("\n\tstartNATS(cfg)\n\n\t// The process is the server now\n", "\n")
	anchor := addSpanAnchor("cmd/web/main.go")
	if anchor.after == "" && anchor.before == "" {
		t.Fatal("fixture assumption broken: no anchor resolved for cmd/web/main.go")
	}
	if _, err := findAnchor(dst, anchor); err == nil {
		t.Fatalf("expected a loud failure when the anchor line %q is absent", anchor.after+anchor.before)
	} else if !strings.Contains(err.Error(), "cannot locate insertion anchor") {
		t.Fatalf("error must name the cause for an operator: %v", err)
	}
}

// TestSpikeAnchorIsExactLineEquality records WHY it fails loudly: the match is
// exact line equality. Re-indenting the anchor line — a change that alters
// nothing semantically — defeats it completely. Loud, but it means the anchor is
// a whole-line contract with the core's formatting, so any reformat is a
// breaking change for every unit sharing that anchor.
func TestSpikeAnchorIsExactLineEquality(t *testing.T) {
	// Same text as mainCallsAnchor with one extra leading tab.
	dst := strings.Split("\t\t\tdefer stopLifecycle()\n", "\n")
	if _, err := findAnchor(dst, addSpanAnchor("cmd/web/main.go")); err == nil {
		t.Fatal("expected re-indentation alone to defeat the anchor")
	}
}

// TestSpikeSpanPresentIsLineSetNotSequence records the SILENT failure mode, and
// it is the one that matters for third-party distribution.
//
// spanPresent decides "already installed" by checking that every span line
// exists somewhere in the target file — a set membership test. It does not check
// order, adjacency or nesting. So a target that contains the same lines in a
// different arrangement is reported as "span already present", and `add` becomes
// a no-op with no error and no receipt entry.
func TestSpikeSpanPresentIsLineSetNotSequence(t *testing.T) {
	// A realistic span: an if/else block.
	span := []string{
		"\t\t\tif signals.DagNatsEnabled {",
		"\t\t\t\tOr open Demo mode.",
		"\t\t\t}",
	}
	// The same three lines, reordered: the closing brace now precedes the body.
	// Set-identical, structurally different, semantically wrong.
	reordered := []string{
		"\t\t\tif signals.DagNatsEnabled {",
		"\t\t\t}",
		"\t\t\t\tOr open Demo mode.",
	}
	if !spanPresent(reordered, span) {
		t.Fatal("expected the line-set check to report present — that IS the finding")
	}
	t.Log("finding: a refactor that keeps the lines but changes their order makes `add` a silent no-op")
}

// TestSpikeDuplicateAnchorTakesFirstMatch records the other silent hazard: the
// anchor search returns the FIRST exact match. A file that gains a second
// identical line makes the insertion position depend on document order rather
// than intent, and no gate reports it.
func TestSpikeDuplicateAnchorTakesFirstMatch(t *testing.T) {
	// Exact equality is the rule, so the duplicate must be byte-identical.
	line := "\t_ = components.Layout(ctx, signals)"
	anchor := anchorBefore(line)
	dst := strings.Split(
		"\t// header copy\n"+
			line+"\n"+
			"\t_ = components.TodoListRegion(signals)\n"+
			"\t// card copy\n"+
			line+"\n", "\n")
	at, err := findAnchor(dst, anchor)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dst[at] != line {
		t.Fatalf("expected the FIRST match (index 1), got %d = %q", at, dst[at])
	}
	t.Log("finding: with two identical candidate lines, insertion position is document order, not intent")
}

// TestSpikeAnchorTableNamesAFileItNoLongerFinds lived here: it logged the two
// dead .templ/handler anchors. The replacement is TestEveryDeclaredAddAnchorIsLive,
// which walks every unit's rule table instead of a three-row sample and fails
// (not logs) on an anchor that is not a live whole line. The dead entries are
// deleted with the table; the finding is recorded in
// docs/research/extensibility-2026-11.md (Spike 0).

// readRepoFile reads a path relative to the repo root, reporting absence rather
// than failing so a probe can log it.
func readRepoFile(t *testing.T, rel string) (string, bool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(rel)))
	if err != nil {
		return "", false
	}
	return string(raw), true
}
