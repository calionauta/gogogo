// Installer engine: trim-provenance tests
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
	got := removedIDs([]builtinUnit{{id: "whiteboard"}, {id: "sounds"}, {id: "dagnats"}})
	want := []string{"dagnats", "sounds", "whiteboard"}
	if !slices.Equal(got, want) {
		t.Errorf("removedIDs = %v, want %v", got, want)
	}
}

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

func TestWriteAgentsPointsUpstream(t *testing.T) {
	dir := t.TempDir()
	// Sorted, as removedIDs produces it in the real apply path.
	drop := []builtinUnit{findUnit(t, "sounds"), findUnit(t, "whiteboard")}
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
