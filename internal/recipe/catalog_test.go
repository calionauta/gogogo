// The catalog entry's review rules, red-proofed.
package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// validCatalogEntry is the baseline every rule test mutates: a reviewed pomodoro
// version with one file, one route, one table, one job and one slot.
func validCatalogEntry() CatalogEntry {
	deps := []string{"google.golang.org/genai"}
	return CatalogEntry{
		ID:        "pomodoro",
		Summary:   "A pomodoro timer with one page and one session table.",
		Version:   "v1.2.0",
		Source:    Upstream{Repo: "github.com/calionauta/gogogo", Ref: "v0.56.0", Path: "features/pomodoro"},
		Files:     map[string]string{"features/pomodoro/handlers/chi.go": strings.Repeat("a", 64)},
		License:   "MIT",
		MinCore:   "v0.56.0",
		WorksWith: []string{"core", "core+todo"},
		Slots:     []string{"settings.section", "nav.sidebar"},
		Routes: []Route{
			{Method: "GET", Pattern: "/pomodoro"},
			{Method: "POST", Pattern: "/api/pomodoro/start"},
		},
		Actions: []InvokeAction{{
			Verb:        "start",
			Description: "Start a pomodoro session.",
			Route:       Route{Method: "POST", Pattern: "/api/pomodoro/start"},
			Params:      []string{"secs"},
			ParamHelp:   map[string]string{"secs": "Seconds to run."},
		}},
		Metrics: []Metric{{
			Label: "Sessions", Query: "SELECT COUNT(*) FROM ext_pomodoro_session WHERE owner = ?",
		}},
		Panel: &PanelSpec{
			Component: "pomodoro_list",
			Prompt:    "pomodoro_list {scope: all|mine} whenever the user asks",
			Writable:  []string{"scope"},
		},
		Tables:          []string{"ext_pomodoro_session"},
		Jobs:            []string{"ext:pomodoro:tick"},
		UpstreamDeps:    &deps,
		ReviewedAt:      "2026-11-14",
		ReviewedBy:      "calionauta",
		ReviewedAgainst: "3f8a1c2",
	}
}

func TestCatalogEntryAcceptsAValidEntry(t *testing.T) {
	if problems := validCatalogEntry().Validate(); len(problems) != 0 {
		t.Fatalf("a valid entry must pass, got %v", problems)
	}
}

// TestCatalogEntryRules mutates one field at a time. Each case names the
// sentence a reviewer would have to write, so the failure explains the rule.
func TestCatalogEntryRules(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*CatalogEntry)
		want   string
	}{
		{"id not lower-case", func(e *CatalogEntry) { e.ID = "Pomodoro" }, "must be lower-case"},
		{"version not semver", func(e *CatalogEntry) { e.Version = "1.2" }, "must be vMAJOR.MINOR.PATCH"},
		{"no repo", func(e *CatalogEntry) { e.Source.Repo = "" }, "source.repo is empty"},
		{"no ref", func(e *CatalogEntry) { e.Source.Ref = "" }, "source.ref is empty"},
		{"no files", func(e *CatalogEntry) { e.Files = nil }, "files is empty"},
		{"bad hash", func(e *CatalogEntry) {
			e.Files["features/pomodoro/handlers/chi.go"] = "deadbeef"
		}, "not a sha256 digest"},
		{"unsafe path", func(e *CatalogEntry) { e.Files["../escape.go"] = strings.Repeat("a", 64) }, "clean repo-relative"},
		{"no license", func(e *CatalogEntry) { e.License = "" }, "license is required"},
		{"no minCore", func(e *CatalogEntry) { e.MinCore = "" }, "minCore is required"},
		{"no deps declaration", func(e *CatalogEntry) { e.UpstreamDeps = nil }, "upstreamDeps is required"},
		{"no review", func(e *CatalogEntry) { e.ReviewedBy = "" }, "reviewedAt, reviewedBy and reviewedAgainst"},
		{"core table", func(e *CatalogEntry) { e.Tables = []string{"todos"} }, "is a core table"},
		{"unprefixed table", func(e *CatalogEntry) {
			e.Tables = []string{"pomodoro_session"}
		}, "must be prefixed ext_pomodoro"},
		{"unprefixed job", func(e *CatalogEntry) { e.Jobs = []string{"tick"} }, "must be prefixed ext:pomodoro"},
		{"unknown slot", func(e *CatalogEntry) { e.Slots = []string{"footer"} }, "not offered by this build"},
		// The review-side half of the no-host rule no longer has a case in
		// this build: every shaped slot has a host page now (dashboard.card
		// is hosted by features/dashboard), so "not hosted" cannot be
		// produced by a declaration naming a real slot. The unknown-slot
		// case above is the refusal that remains.
		{"empty worksWith", func(e *CatalogEntry) { e.WorksWith = []string{""} }, "worksWith holds an empty set name"},
		{"missing summary", func(e *CatalogEntry) { e.Summary = "" }, "summary is required"},
		{"action without description", func(e *CatalogEntry) { e.Actions[0].Description = "" }, "has no description"},
		{"action targets undeclared route", func(e *CatalogEntry) {
			e.Actions[0].Route = Route{Method: "POST", Pattern: "/api/ghost"}
		}, "targets undeclared route"},
		{"action help outside params", func(e *CatalogEntry) {
			e.Actions[0].ParamHelp["ghost"] = "not accepted"
		}, "documents param \"ghost\""},
		{"non-count metric", func(e *CatalogEntry) {
			e.Metrics = []Metric{{Label: "Sessions", Query: "SELECT id FROM ext_pomodoro_session"}}
		}, "must select count"},
		{"bad panel name", func(e *CatalogEntry) { e.Panel.Component = "PomodoroList" }, "must be snake_case"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := validCatalogEntry()
			tc.mutate(&e)
			problems := e.Validate()
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Fatalf("expected a problem containing %q, got %v", tc.want, problems)
			}
		})
	}
}

// TestIsValidVersion pins the one shape validator: catalog entries and the
// installable manifest must agree on what a version is.
func TestIsValidVersion(t *testing.T) {
	for _, v := range []string{"v0.1.0", "v1.2.0", "v1.2.3-rc.1", "v10.20.30"} {
		if !IsValidVersion(v) {
			t.Errorf("IsValidVersion(%q) must be true", v)
		}
	}
	for _, v := range []string{"", "1.2.0", "v1.2", "v1", "main", "0.53.0"} {
		if IsValidVersion(v) {
			t.Errorf("IsValidVersion(%q) must be false", v)
		}
	}
}

// TestScanCatalogGroupsEntriesByID: a catalog directory with two versions of
// one recipe loads both, grouped, so the caller's ordering sees the whole
// history.
func TestScanCatalogGroupsEntriesByID(t *testing.T) {
	dir := t.TempDir()
	for _, version := range []string{"v1.2.0", "v1.10.0"} {
		e := validCatalogEntry()
		e.Version = version
		writeCatalogEntryFile(t, dir, e)
	}
	got, err := ScanCatalog(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(got["pomodoro"]) != 2 {
		t.Fatalf("want 2 pomodoro entries, got %+v", got)
	}
	found := map[string]bool{}
	for _, e := range got["pomodoro"] {
		found[e.Version] = true
	}
	if !found["v1.2.0"] || !found["v1.10.0"] {
		t.Fatalf("both versions must load: %v", found)
	}
}

// TestScanCatalogMissingDirIsEmptyNoError pins that absence is not failure:
// a project without a catalog is every recipe unknown, not a broken install.
func TestScanCatalogMissingDirIsEmptyNoError(t *testing.T) {
	got, err := ScanCatalog(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("missing catalog dir must not fail: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("missing catalog must scan as empty, got %+v", got)
	}
}

// TestScanCatalogRejectsAnInvalidEntry: a present but unreadable review
// fails at scan time, with the file named.
func TestScanCatalogRejectsAnInvalidEntry(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "pomodoro", "v1.0.0.yaml")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("id: pomodoro\nversion: v1.0.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanCatalog(dir); err == nil || !strings.Contains(err.Error(), "v1.0.0.yaml") {
		t.Fatalf("invalid entry must fail naming the file, got %v", err)
	}
}

// TestScanCatalogRejectsAnIDMismatch: an entry sitting under another
// recipe's directory is a layout bug, never a silent merge.
func TestScanCatalogRejectsAnIDMismatch(t *testing.T) {
	dir := t.TempDir()
	raw, err := yaml.Marshal(validCatalogEntry())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "timer", "v1.2.0.yaml")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanCatalog(dir); err == nil || !strings.Contains(err.Error(), "timer") {
		t.Fatalf("an entry under the wrong id directory must fail, got %v", err)
	}
}

// writeCatalogEntryFile commits one valid entry at catalog-dir/<id>/<version>.yaml.
func writeCatalogEntryFile(t *testing.T, dir string, e CatalogEntry) {
	t.Helper()
	raw, err := yaml.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, e.ID, e.Version+".yaml")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestCatalogEntryRoundTripsThroughYAML pins the artifact, not just the struct:
// the file a reviewer commits must load back to the same claims.
func TestCatalogEntryRoundTripsThroughYAML(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "entry.yml")
	body := `id: pomodoro
summary: A pomodoro timer with one page and one session table.
version: v1.2.0
source:
  repo: github.com/calionauta/gogogo
  ref: v0.56.0
  path: features/pomodoro
files:
  features/pomodoro/handlers/chi.go: sha256:` + strings.Repeat("a", 64) + `
license: MIT
minCore: v0.56.0
worksWith: [core, core+todo]
slots: [settings.section]
routes:
  - {method: GET, pattern: /pomodoro}
  - {method: POST, pattern: /api/pomodoro/start}
actions:
  - verb: start
    description: Start a pomodoro session.
    route: {method: POST, pattern: /api/pomodoro/start}
    params: [secs]
    paramHelp: {secs: Seconds to run.}
metrics:
  - {label: Sessions, query: "SELECT COUNT(*) FROM ext_pomodoro_session WHERE owner = ?"}
panel:
  component: pomodoro_list
  prompt: "pomodoro_list {scope: all|mine} whenever the user asks"
  writable: [scope]
tables: [ext_pomodoro_session]
jobs: [ext:pomodoro:tick]
upstreamDeps: []
reviewedAt: "2026-11-14"
reviewedBy: calionauta
reviewedAgainst: 3f8a1c2
`
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	e, err := LoadCatalogEntry(file)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if e.ID != "pomodoro" || e.Version != "v1.2.0" || len(e.Files) != 1 {
		t.Fatalf("round trip lost fields: %+v", e)
	}
	if len(e.Actions) != 1 || e.Actions[0].Description == "" || e.Actions[0].ParamHelp["secs"] == "" {
		t.Fatalf("round trip lost the invocable surface: %+v", e.Actions)
	}
	if len(e.Metrics) != 1 || e.Panel == nil {
		t.Fatalf("round trip lost metrics/panel: metrics=%+v panel=%+v", e.Metrics, e.Panel)
	}
	if e.UpstreamDeps == nil || len(*e.UpstreamDeps) != 0 {
		t.Fatalf("an empty upstreamDeps list must survive as a declared no-dependency claim: %+v", e.UpstreamDeps)
	}
}

// TestCatalogEntryRejectsAnInvalidFile pins the load-time refusal: a broken
// artifact fails before an install can act on it.
func TestCatalogEntryRejectsAnInvalidFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "entry.yml")
	if err := os.WriteFile(file, []byte("id: pomodoro\nversion: v1.0.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCatalogEntry(file); err == nil || !strings.Contains(err.Error(), "invalid catalog entry") {
		t.Fatalf("an incomplete entry must fail at load, got %v", err)
	}
}
