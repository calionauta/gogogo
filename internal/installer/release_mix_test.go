// SCOPE:plugin — tests for the cross-release guard around `add`
package installer

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// changelog is the shape parseChangelog reads: release-please's newest-first
// sections.
const changelog = `# Changelog

## 0.55.0 (2026-10-22)

### Features

* a newer thing

## 0.54.0 (2026-10-21)

### Fixes

* an older thing
`

// projectAt builds a scaffolded-project fixture: just the marker the guard
// reads. `add` needs much more of a tree, but the guard does not.
func projectAt(t *testing.T, ref string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "AGENTS.md"),
		"# demo\n\nscaffolded-from: "+ref+"\ntemplate-commit: deadbee\n")
	return dir
}

// templateAt builds a template-checkout fixture with a changelog.
func templateAt(t *testing.T, version string) string {
	t.Helper()
	dir := t.TempDir()
	if version != "" {
		writeFile(t, filepath.Join(dir, "CHANGELOG.md"), changelog)
	}
	return dir
}

func TestSourceReleaseReadsTheTopChangelogEntry(t *testing.T) {
	if got := sourceRelease(templateAt(t, "any")); got != "0.55.0" {
		t.Errorf("sourceRelease = %q, want 0.55.0", got)
	}
	// No changelog is not a release: unknown must stay unknown.
	if got := sourceRelease(templateAt(t, "")); got != "" {
		t.Errorf("sourceRelease without a changelog = %q, want empty", got)
	}
}

func TestProjectBaseReadsTheMarker(t *testing.T) {
	cases := map[string]string{
		"calionauta/gogogo@v0.53.0": "0.53.0",
		"calionauta/gogogo@0.53.0":  "0.53.0",
		"calionauta/gogogo@dev":     "",
	}
	for ref, want := range cases {
		if got := projectBase(projectAt(t, ref)); got != want {
			t.Errorf("projectBase(%q) = %q, want %q", ref, got, want)
		}
	}
	// No marker at all (a tree that predates provenance) is unknown, not zero.
	if got := projectBase(t.TempDir()); got != "" {
		t.Errorf("projectBase without a marker = %q, want empty", got)
	}
}

// TestCheckReleaseMixRefusesAcrossReleases is the guard's point: the copy must
// stop, name both releases, and give the two ways out.
func TestCheckReleaseMixRefusesAcrossReleases(t *testing.T) {
	project := projectAt(t, "calionauta/gogogo@v0.53.0")
	template := templateAt(t, "0.55.0")

	var note strings.Builder
	err := checkReleaseMix(project, template, &note)
	if err == nil {
		t.Fatal("copying across releases must be refused")
	}
	for _, want := range []string{"v0.55.0", "v0.53.0", "git -C", "checkout v0.53.0", "gogogo migrate"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not name %q", err, want)
		}
	}
	if note.String() != "" {
		t.Errorf("a refusal must not also print a skip note: %q", note.String())
	}
}

func TestCheckReleaseMixAllowsTheSameRelease(t *testing.T) {
	project := projectAt(t, "calionauta/gogogo@v0.55.0")
	var note strings.Builder
	if err := checkReleaseMix(project, templateAt(t, "0.55.0"), &note); err != nil {
		t.Fatalf("same release refused: %v", err)
	}
	if note.String() != "" {
		t.Errorf("same release should print nothing, got %q", note.String())
	}
}

// TestCheckReleaseMixAnnouncesWhenItCannotTell keeps "nothing to check" from
// reading as "checked and fine".
func TestCheckReleaseMixAnnouncesWhenItCannotTell(t *testing.T) {
	var note strings.Builder
	if err := checkReleaseMix(projectAt(t, "calionauta/gogogo@v0.53.0"), templateAt(t, ""), &note); err != nil {
		t.Fatalf("unknown template release must not refuse: %v", err)
	}
	if !strings.Contains(note.String(), "release check skipped") ||
		!strings.Contains(note.String(), "unknown") {
		t.Errorf("skip must be announced and name what is unknown, got %q", note.String())
	}
}

// TestRunAddRefusesAcrossReleasesWireUp proves the guard is actually CALLED. A
// unit test of checkReleaseMix cannot catch a missing call site, and the whole
// point of this check is that its absence is silent.
func TestRunAddRefusesAcrossReleasesWireUp(t *testing.T) {
	project := t.TempDir()
	writeFile(t, filepath.Join(project, "AGENTS.md"),
		"# demo\n\nscaffolded-from: calionauta/gogogo@v0.50.0\n")
	template := templateAt(t, "0.55.0")

	var out strings.Builder
	err := runAdd(context.Background(), []string{
		"sounds", "--from", template, "--dir", project, "--dry-run",
	}, strings.NewReader(""), &out)
	if err == nil {
		t.Fatalf("add must refuse a cross-release copy; it printed:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "mixes two trees") {
		t.Errorf("refusal %q is not the release guard", err)
	}
}
