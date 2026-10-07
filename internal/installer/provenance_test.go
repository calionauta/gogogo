// SCOPE:layer=infra,removal=plugin — installer engine: trim-provenance tests
package installer

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestRemovedIDsAreSorted pins the ordering that keeps the AGENTS.md line
// stable: an unsorted list would show as a diff on every scaffold, and a diff in
// a machine-read line is exactly what erodes trust in it.
func TestRemovedIDsAreSorted(t *testing.T) {
	got := removedIDs([]trimUnit{{id: "whiteboard"}, {id: "sounds"}, {id: "dagnats"}})
	want := []string{"dagnats", "sounds", "whiteboard"}
	if !slices.Equal(got, want) {
		t.Errorf("removedIDs = %v, want %v", got, want)
	}
}

// TestCheckAcceptsDeliberatelyTrimmedTree is the regression test for the
// misleading verdict: against a scaffolded tree every marker is legitimately
// absent, so the strict path reported CHECK-FAIL + exit 1 for a tree that is
// exactly what the user asked for. An LLM agent reads exit 1 + FAIL as "the
// scaffold is broken". The recorded provenance must reclassify those units.
func TestCheckAcceptsDeliberatelyTrimmedTree(t *testing.T) {
	dir := t.TempDir()
	scaffoldMinimalTree(t, dir)
	// The tree records that whiteboard was removed on purpose.
	if err := writeAgents(dir, "my-app", []string{"whiteboard"}); err != nil {
		t.Fatal(err)
	}

	var buf strings.Builder
	printCheck(&buf, checkTree(dir), dir)
	out := buf.String()

	// The unit recorded as removed is reclassified, NOT reported as failure.
	// (This tree also lacks other units it never recorded, and those correctly
	// stay CHECK-FAIL — see TestCheckStillFailsWithoutProvenance. What is under
	// test here is only that a RECORDED removal stops being an error, which is
	// what made an LLM read a correct scaffold as broken.)
	if strings.Contains(out, "CHECK-FAIL whiteboard") {
		t.Errorf("a deliberately trimmed unit must not be CHECK-FAIL:\n%s", out)
	}
	if !strings.Contains(out, "CHECK-TRIMMED whiteboard") {
		t.Errorf("expected CHECK-TRIMMED for whiteboard:\n%s", out)
	}
}

// TestExplainTrimCountsOnlyUnrecordedUnits is the precise assertion the
// end-to-end test above cannot make on a minimal tree: a recorded removal must
// not count toward the failure total, while an unrecorded one must.
func TestExplainTrimCountsOnlyUnrecordedUnits(t *testing.T) {
	results := []unitCheck{
		{ID: "dagnats", Problems: []string{"missing dir internal/dagnats"}},
		{ID: "whiteboard", Problems: []string{"missing dir features/whiteboard"}},
		{ID: "sounds"}, // applies cleanly
	}
	tp := trimProvenance{found: true, removed: map[string]bool{"whiteboard": true}}

	var buf strings.Builder
	failed := explainTrim(&buf, results, tp)
	out := buf.String()

	if failed != 1 {
		t.Errorf("failed = %d, want 1 (only dagnats is unrecorded)", failed)
	}
	if !strings.Contains(out, "CHECK-TRIMMED whiteboard") {
		t.Errorf("whiteboard must be CHECK-TRIMMED:\n%s", out)
	}
	if !strings.Contains(out, "CHECK-FAIL dagnats") {
		t.Errorf("dagnats must stay CHECK-FAIL:\n%s", out)
	}
	if !strings.Contains(out, "CHECK-OK sounds") {
		t.Errorf("sounds must be CHECK-OK:\n%s", out)
	}
}

// TestReadTrimProvenanceIgnoresUnknownIDs: the removed: line is read from a
// markdown file a human may edit, so an id the manifest does not know must be
// ignored rather than trusted as a verdict.
func TestReadTrimProvenanceIgnoresUnknownIDs(t *testing.T) {
	dir := t.TempDir()
	body := "# AGENTS\n\n## Trim provenance\n\nremoved: whiteboard, not-a-real-unit\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	tp := readTrimProvenance(dir)
	if !tp.found {
		t.Fatal("expected provenance to be found")
	}
	if !tp.removed["whiteboard"] {
		t.Error("a known id must be recorded")
	}
	if tp.removed["not-a-real-unit"] {
		t.Error("an unknown id must be ignored, not trusted")
	}
}

// TestReadTrimProvenanceMissingSection: no section means no provenance, so
// --check must fall back to strict rather than assume "nothing was trimmed".
func TestReadTrimProvenanceMissingSection(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# AGENTS\n\nnothing here\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if tp := readTrimProvenance(dir); tp.found {
		t.Error("a file with no provenance section must report found=false")
	}
}

// TestCheckStillFailsWithoutProvenance is the safety half: with no recorded
// trim the check must stay strict, because a false "this is fine" is worse than
// a noisy failure — the gate exists to catch drift.
func TestCheckStillFailsWithoutProvenance(t *testing.T) {
	dir := t.TempDir()
	scaffoldMinimalTree(t, dir) // no AGENTS.md at all

	var buf strings.Builder
	failed := printCheck(&buf, checkTree(dir), dir)
	if failed == 0 {
		t.Errorf("expected failures without provenance, got 0:\n%s", buf.String())
	}
}

// scaffoldMinimalTree writes just enough of the template layout that
// looksLikeTemplate is satisfied and the manifest markers are missing.
func scaffoldMinimalTree(t *testing.T, dir string) {
	t.Helper()
	for _, p := range []string{"cmd/web/main.go", "internal/installer/run.go"} {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("package main\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWriteAgentsPointsUpstream(t *testing.T) {
	dir := t.TempDir()
	// Sorted, as removedIDs produces it in the real apply path.
	if err := writeAgents(dir, "my-app", []string{"sounds", "whiteboard"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	text := string(raw)
	for _, want := range []string{
		"my-app", "llms.txt", "scope-taxonomy", "blob/master",
		// The machine-read provenance line, which --check depends on. Losing it
		// silently would make every trimmed tree report as manifest drift again.
		"removed: sounds, whiteboard",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("AGENTS.md missing %q:\n%s", want, text)
		}
	}
}

// TestWriteAgentsRecordsEmptyTrim covers the no-trim case: the line must still
// be present and parseable (as "none"), because an absent line reads as "no
// provenance" and silently drops the tree back to strict checking.
func TestWriteAgentsRecordsEmptyTrim(t *testing.T) {
	dir := t.TempDir()
	if err := writeAgents(dir, "my-app", nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if !strings.Contains(string(raw), "removed: none") {
		t.Errorf("AGENTS.md must record an empty trim as 'removed: none':\n%s", raw)
	}
	// And a full template must still read as having provenance, so --check can
	// trust it rather than falling back to strict.
	if tp := readTrimProvenance(dir); !tp.found {
		t.Error("readTrimProvenance found no provenance for a full-template AGENTS.md")
	}
}
