// SCOPE:plugin — installer engine: migration range tests
package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// changelogFixture mirrors the shape release-please actually writes, including
// the two things a naive parser gets wrong: the "## What's Changed" H2 inside
// every section (it is not a release) and the "**Full Changelog**" trailer (it
// is not a bullet).
const changelogFixture = `# Changelog

Prose before the first release, which no parser may treat as entries.
* not an entry either

## 0.53.0 (2026-10-21)

## What's Changed
* feat(ui): shadcn skin and skin resolution by @calionauta in https://example/199
* fix(server): close the origin-guard edge cases by @calionauta in https://example/203


**Full Changelog**: https://example/compare/v0.52.0...v0.53.0

## 0.52.0 (2026-10-20)

## What's Changed
* feat(agents): unit contracts and a docs claim gate by @calionauta in https://example/186

## 0.51.0 (2026-10-19)

## What's Changed
* fix(realtime): correct collaborative presence by @calionauta in https://example/182

## 0.50.0 (2026-10-18)

## What's Changed
* feat(search): three layers behind one seam by @calionauta in https://example/170

## [0.49.0] - 2026-10-17

### Features
* **dagnats:** pin the workflow engine by @calionauta in https://example/165

## [0.1.0] - Initial release

* first cut of the template
`

// TestParseChangelogIgnoresHeadingsAndTrailers is the parse contract: an H2
// inside a section is not a release, and the trailer is not an entry.
func TestParseChangelogIgnoresHeadingsAndTrailers(t *testing.T) {
	releases := parseChangelog(changelogFixture)
	// Six: four release-please sections plus two of the older
	// Keep-a-Changelog shape, one of which has no date at all.
	if len(releases) != 6 {
		t.Fatalf("parsed %d releases, want 6: %+v", len(releases), releases)
	}
	if releases[0].Version != "0.53.0" || releases[0].Date != "2026-10-21" {
		t.Errorf("newest release = %+v", releases[0])
	}
	if len(releases[0].Entries) != 2 {
		t.Errorf("0.53.0 entries = %d, want 2: %v", len(releases[0].Entries), releases[0].Entries)
	}
	if releases[3].Version != "0.50.0" {
		t.Errorf("fourth release = %q", releases[3].Version)
	}
	// The older heading shape: "## [0.49.0] - 2026-10-17". It must parse, and
	// its date must survive the different separator.
	if releases[4].Version != "0.49.0" {
		t.Errorf("bracketed release = %q, want 0.49.0 (the older heading shape)", releases[4].Version)
	}
	if releases[4].Date != "2026-10-17" {
		t.Errorf("bracketed release date = %q, want 2026-10-17", releases[4].Date)
	}
	if releases[5].Version != "0.1.0" || releases[5].Date != "" {
		t.Errorf("undated release = %+v, want 0.1.0 with no date", releases[5])
	}
	for _, r := range releases {
		for _, e := range r.Entries {
			// The two things that must never become entries: the compare
			// trailer and the prose above the first section.
			if strings.HasPrefix(e, "Full Changelog") || e == "not an entry either" {
				t.Errorf("chrome leaked into entries: %q", e)
			}
		}
	}
	// The older sections' bullets are real entries and must survive verbatim.
	if len(releases[4].Entries) != 1 || !strings.HasPrefix(releases[4].Entries[0], "**dagnats:**") {
		t.Errorf("older-shape entries = %v", releases[4].Entries)
	}
}

// TestMigrationRangeIsExclusiveFromInclusiveTo: the base revision is already in
// the tree, so its own release must not be listed as a change to apply.
func TestMigrationRangeIsExclusiveFromInclusiveTo(t *testing.T) {
	all := parseChangelog(changelogFixture)
	got := migrationRange(all, "0.50.0", "0.52.0")
	if len(got) != 2 {
		t.Fatalf("range 0.50.0→0.52.0 = %d releases, want 2: %+v", len(got), got)
	}
	if got[0].Version != "0.52.0" || got[1].Version != "0.51.0" {
		t.Errorf("range = %v (want 0.52.0, 0.51.0)", got)
	}
	// An incomparable base filters nothing rather than dropping everything —
	// the "--from dev" case, where silence would read as "nothing changed".
	// The upper bound still applies, so 0.52.0 and 0.51.0 AND 0.50.0 stay: with
	// no base there is no way to know which of them the tree already has, and
	// listing one too many is the honest direction.
	// Named, not shadowed: the loop below must check THIS slice. A shadowed
	// `got` made it validate the other range, which is a test that cannot fail.
	loose := migrationRange(all, "dev", "0.52.0")
	if len(loose) != 5 {
		t.Errorf("incomparable base kept %d, want 5", len(loose))
	}
	// Newest first, regardless of how the file orders its own tail.
	for i := 1; i < len(loose); i++ {
		if cmp, ok := versionOrder(loose[i-1].Version, loose[i].Version); !ok || cmp < 0 {
			t.Errorf("range is not newest-first at %d: %q then %q",
				i, loose[i-1].Version, loose[i].Version)
		}
	}
}

// TestTagRefNormalizesReleaseTags covers the two spellings a caller may pass.
func TestTagRefNormalizesReleaseTags(t *testing.T) {
	for in, want := range map[string]string{
		"0.53.0": "v0.53.0", "v0.53.0": "v0.53.0", "": "",
	} {
		if got := tagRef(in); got != want {
			t.Errorf("tagRef(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRunMigrateReadsALocalChangelog is the offline path: no network, an
// explicit file, a full report out.
func TestRunMigrateReadsALocalChangelog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CHANGELOG.md")
	if err := os.WriteFile(path, []byte(changelogFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err := runMigrate(context.Background(), []string{
		"--from", "v0.50.0", "--to", "v0.52.0", "--changelog", path, "--format", "json",
	}, &buf)
	if err != nil {
		t.Fatalf("runMigrate: %v", err)
	}
	var rep migrationReport
	if jerr := json.Unmarshal(buf.Bytes(), &rep); jerr != nil {
		t.Fatalf("json does not parse: %v\n%s", jerr, buf.String())
	}
	if rep.From != "0.50.0" || rep.To != "v0.52.0" {
		t.Errorf("bounds = %q → %q", rep.From, rep.To)
	}
	if len(rep.Releases) != 2 {
		t.Errorf("releases = %d, want 2: %+v", len(rep.Releases), rep.Releases)
	}
	if len(rep.Gates) == 0 {
		t.Error("the report must carry the gates that prove the result")
	}
	if !strings.HasPrefix(rep.Source, "file:") {
		t.Errorf("source = %q, want file:<path>", rep.Source)
	}
}

// TestRunMigrateUsesTheRecordedBase is the payoff of the marker: no --from,
// and the base comes from the tree itself.
func TestRunMigrateUsesTheRecordedBase(t *testing.T) {
	dir := t.TempDir()
	writeMarkerFixture(t, dir, "calionauta/gogogo@v0.50.0")
	path := filepath.Join(t.TempDir(), "CHANGELOG.md")
	if err := os.WriteFile(path, []byte(changelogFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := runMigrate(context.Background(), []string{
		"--dir", dir, "--to", "v0.53.0", "--changelog", path,
	}, &buf); err != nil {
		t.Fatalf("runMigrate: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "0.50.0 → 0.53.0") {
		t.Errorf("text must state the range it resolved:\n%s", out)
	}
	if !strings.Contains(out, "0.53.0 (2026-10-21)") {
		t.Errorf("text must list the newest release:\n%s", out)
	}
	if !strings.Contains(out, "make ci-local-fast") {
		t.Errorf("text must print the proof gates:\n%s", out)
	}
}

// TestRunMigrateWithoutABaseFailsWithGuidance: no marker and no --from is a
// usage question, so it must say what to pass — not guess a base.
func TestRunMigrateWithoutABaseFailsWithGuidance(t *testing.T) {
	var buf bytes.Buffer
	err := runMigrate(context.Background(), []string{"--dir", t.TempDir(), "--to", "v0.53.0"}, &buf)
	if err == nil {
		t.Fatal("expected an error for a missing base")
	}
	if !strings.Contains(err.Error(), "--from") {
		t.Errorf("error must name the flag that fixes it: %v", err)
	}
}

// TestRunMigrateReportsNothingNew: a project already on the latest release gets
// a sentence, not an empty document.
func TestRunMigrateReportsNothingNew(t *testing.T) {
	dir := t.TempDir()
	writeMarkerFixture(t, dir, "calionauta/gogogo@v0.53.0")
	path := filepath.Join(t.TempDir(), "CHANGELOG.md")
	if err := os.WriteFile(path, []byte(changelogFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := runMigrate(context.Background(), []string{
		"--dir", dir, "--to", "v0.53.0", "--changelog", path,
	}, &buf); err != nil {
		t.Fatalf("runMigrate: %v", err)
	}
	// The no-op sentence must name the source it looked at: with --changelog
	// pointing at a stale file, "you are current" is a claim about the file.
	if !strings.Contains(buf.String(), "no newer releases between 0.53.0 and v0.53.0") {
		t.Errorf("expected an explicit no-op sentence:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "in file:") {
		t.Errorf("the no-op sentence must name its source:\n%s", buf.String())
	}
}

// TestParseChangelogReadsTheRealFile is the regression test the fixture alone
// could not be: this repo's CHANGELOG.md carries TWO heading shapes (81
// "## [x.y.z] - date" sections from the Keep-a-Changelog era, and the newer
// "## x.y.z (date)" release-please form). A parser that knows only the current
// shape drops most of the history SILENTLY, which is the worst failure mode for
// a migration list — it looks like "nothing changed".
func TestParseChangelogReadsTheRealFile(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "CHANGELOG.md"))
	if err != nil {
		t.Skipf("no changelog beside the package (%v)", err)
	}
	releases := parseChangelog(string(raw))
	if len(releases) < 100 {
		t.Fatalf("parsed %d releases from the real changelog — the old heading "+
			"shape is being dropped silently", len(releases))
	}
	for _, r := range releases {
		if _, ok := parseSemver(r.Version); !ok {
			t.Fatalf("unparseable version in the real changelog: %q", r.Version)
		}
	}
	// The file's tail is NOT ordered (0.1.0 sits above 0.24.10), which is
	// exactly why migrationRange sorts instead of trusting file order. The
	// parse just has to not lose it.
	sawOldest := false
	for _, r := range releases {
		if r.Version == "0.1.0" {
			sawOldest = true
		}
	}
	if !sawOldest {
		t.Error("the 0.1.0 section is missing — the oldest heading shape is being dropped")
	}
	if releases[0].Version != "0.53.0" {
		t.Errorf("newest release = %q, want 0.53.0 at the top", releases[0].Version)
	}
}

// TestMigrationRangeSortsByVersion pins the sort directly. The fixture is
// already newest-first (so it cannot detect a missing sort), and the REAL
// changelog is not ordered at its tail — which is why migrationRange sorts
// instead of trusting the file.
func TestMigrationRangeSortsByVersion(t *testing.T) {
	shuffled := []migrationRelease{
		{Version: "0.51.0"},
		{Version: "0.53.0"},
		{Version: "0.52.0"},
	}
	got := migrationRange(shuffled, "0.50.0", "0.53.0")
	want := []string{"0.53.0", "0.52.0", "0.51.0"}
	for i, w := range want {
		if got[i].Version != w {
			t.Fatalf("range order = %v, want %v", versions(got), want)
		}
	}
}

func versions(rs []migrationRelease) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Version)
	}
	return out
}
