// The catalog entry's review rules, red-proofed.
package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validCatalogEntry is the baseline every rule test mutates: a reviewed pomodoro
// version with one file, one route, one table, one job and one slot.
func validCatalogEntry() CatalogEntry {
	deps := []string{"google.golang.org/genai"}
	return CatalogEntry{
		ID:              "pomodoro",
		Version:         "v1.2.0",
		Source:          Upstream{Repo: "github.com/calionauta/gogogo", Ref: "v0.56.0", Path: "features/pomodoro"},
		Files:           map[string]string{"features/pomodoro/handlers/chi.go": strings.Repeat("a", 64)},
		License:         "MIT",
		MinCore:         "v0.56.0",
		WorksWith:       []string{"core", "core+todo"},
		Slots:           []string{"settings.section", "nav.sidebar"},
		Routes:          []Route{{Method: "GET", Pattern: "/pomodoro"}},
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
		// A shape with no host page is the review-side half of the same rule: a
		// reviewed recipe must not claim content nothing draws.
		{"slot with no host page", func(e *CatalogEntry) {
			e.Slots = []string{"dashboard.card"}
		}, "not hosted by this build"},
		{"empty worksWith", func(e *CatalogEntry) { e.WorksWith = []string{""} }, "worksWith holds an empty set name"},
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

// TestCatalogEntryRoundTripsThroughYAML pins the artifact, not just the struct:
// the file a reviewer commits must load back to the same claims.
func TestCatalogEntryRoundTripsThroughYAML(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "entry.yml")
	body := `id: pomodoro
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
