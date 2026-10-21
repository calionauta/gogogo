// SCOPE:plugin — installer engine: trim-provenance tests
package installer

import (
	"context"
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
	if err := writeAgents(context.Background(), dir, "my-app", []trimUnit{findUnit(t, "whiteboard")}); err != nil {
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
	drop := []trimUnit{findUnit(t, "sounds"), findUnit(t, "whiteboard")}
	if err := writeAgents(context.Background(), dir, "my-app", drop); err != nil {
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
	if err := writeAgents(context.Background(), dir, "my-app", nil); err != nil {
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

// --- the base marker: what makes "is this project current?" answerable ---

// TestWriteAgentsRecordsTemplateBase pins both machine-read lines. Losing them
// silently would quietly restore the old behaviour, where --check-update could
// only compare the binary and reported a stale tree as current.
func TestWriteAgentsRecordsTemplateBase(t *testing.T) {
	dir := t.TempDir()
	if err := writeAgents(context.Background(), dir, "my-app", nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	text := string(raw)
	if !strings.Contains(text, "scaffolded-from: calionauta/gogogo@") {
		t.Errorf("AGENTS.md must record the template base:\n%s", text)
	}
	if !strings.Contains(text, "template-commit: ") {
		t.Errorf("AGENTS.md must record a commit line (unknown is a value):\n%s", text)
	}
	// The section that turns the marker into an action a downstream agent can
	// take. Losing it silently leaves the marker describing a command nobody
	// knows about.
	for _, want := range []string{"## Keeping this project current", "gogogo migrate --dir ."} {
		if !strings.Contains(text, want) {
			t.Errorf("AGENTS.md must point at the upgrade commands (%q missing):\n%s", want, text)
		}
	}
}

// TestReadTemplateMarkerRoundTrip: what the installer writes is what the
// comparison and migrate commands read back.
func TestReadTemplateMarkerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	writeMarkerFixture(t, dir, "calionauta/gogogo@v0.50.0")
	m := readTemplateMarker(dir)
	if !m.Found {
		t.Fatal("marker not found")
	}
	if m.Ref != "calionauta/gogogo@v0.50.0" {
		t.Errorf("ref = %q", m.Ref)
	}
	// A non-git tree records "unknown", which must survive the read.
	if m.Commit != "unknown" {
		t.Errorf("commit = %q, want unknown", m.Commit)
	}
}

// TestReadTemplateMarkerAbsentIsNotLatest: the failure mode this guards against
// is a missing marker being read as "current", which would silence the check
// exactly where it matters (trees scaffolded before the marker existed).
func TestReadTemplateMarkerAbsentIsNotLatest(t *testing.T) {
	dir := t.TempDir()
	markerless := []byte("# AGENTS\n\nno marker here\n")
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), markerless, 0o600); err != nil {
		t.Fatal(err)
	}
	if m := readTemplateMarker(dir); m.Found {
		t.Errorf("a markerless AGENTS.md must not report Found: %+v", m)
	}
	// And a truncated/garbage commit must not poison the ref.
	writeMarkerFixture(t, dir, "calionauta/gogogo@v0.50.0")
	garbage := []byte("scaffolded-from: calionauta/gogogo@v0.50.0\ntemplate-commit: not-a-sha\n")
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), garbage, 0o600); err != nil {
		t.Fatal(err)
	}
	m := readTemplateMarker(dir)
	if !m.Found || m.Commit != "unknown" {
		t.Errorf("garbage commit should degrade to unknown, got %+v", m)
	}
}

// TestGitHeadReadsARealRepository proves the commit is recorded as a real
// revision, not a placeholder, when the tree IS a checkout (the normal clone
// path: `git clone --depth 1` keeps .git).
func TestGitHeadReadsARealRepository(t *testing.T) {
	if err := lookGit(); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	ctx := context.Background()
	for _, args := range [][]string{
		{"init", "-q"},
		{
			"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q",
			"--allow-empty", "-m", "scaffold",
		},
	} {
		if out, err := runIn(ctx, dir, "git", args...); err != nil {
			t.Skipf("git unavailable here (%v: %s)", err, out)
		}
	}
	head := gitHead(ctx, dir)
	if len(head) != 40 {
		t.Errorf("gitHead = %q, want a full 40-char SHA", head)
	}
	if head == "unknown" {
		t.Error("gitHead reported unknown inside a real repository")
	}
}

// TestCheckPrintsTheRecordedBase: the drift report must state the revision it
// is relative to, or a reader assumes the tree came from the reviewed one.
func TestCheckPrintsTheRecordedBase(t *testing.T) {
	dir := t.TempDir()
	scaffoldMinimalTree(t, dir)
	writeMarkerFixture(t, dir, "calionauta/gogogo@v0.50.0")
	var buf strings.Builder
	printCheck(&buf, checkTree(dir), dir)
	if !strings.Contains(buf.String(), "CHECK-TREE scaffolded from calionauta/gogogo@v0.50.0") {
		t.Errorf("check output must name the base:\n%s", buf.String())
	}
}
