// `gogogo generate --check`: did generation change the files it owns?
//
// The first implementation ran `git diff --exit-code` with no paths, so the
// answer was "is the working tree dirty" — true on every branch anyone works on.
// These tests pin the question the gate is supposed to ask.
package installer

import (
	"path/filepath"
	"slices"
	"testing"
)

// TestChangedOutputsIgnoresUnrelatedEdits: a README edit is not generation drift.
func TestChangedOutputsIgnoresUnrelatedEdits(t *testing.T) {
	root := t.TempDir()
	owned := filepath.Join(root, "app.min.css")
	writeFile(t, owned, "body{}\n")
	writeFile(t, filepath.Join(root, "README.md"), "hello\n")

	before, err := snapshotOutputs([]string{owned})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	writeFile(t, filepath.Join(root, "README.md"), "hello, world\n")
	dirty, err := changedOutputs(before, []string{owned})
	if err != nil {
		t.Fatalf("changedOutputs: %v", err)
	}
	if len(dirty) != 0 {
		t.Fatalf("an unrelated edit was reported as drift: %v", dirty)
	}
}

// TestChangedOutputsReportsAnOwnedFile covers the real case: the generator
// rewrote what it owns, so the committed copy is stale.
func TestChangedOutputsReportsAnOwnedFile(t *testing.T) {
	root := t.TempDir()
	owned := filepath.Join(root, "app.min.css")
	writeFile(t, owned, "body{}\n")

	before, err := snapshotOutputs([]string{owned})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	writeFile(t, owned, "body{color:red}\n")
	dirty, err := changedOutputs(before, []string{owned})
	if err != nil {
		t.Fatalf("changedOutputs: %v", err)
	}
	if len(dirty) != 1 || dirty[0] != owned {
		t.Fatalf("dirty = %v, want the rewritten file", dirty)
	}
}

// TestExpandPatternReadsADirectoryOfGoFiles: sqlcgen is a directory, and a
// snapshot that stats it as a file fails before the check can answer.
func TestExpandPatternReadsADirectoryOfGoFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sqlcgen")
	writeFile(t, filepath.Join(dir, "models.go"), "package sqlcgen\n")
	writeFile(t, filepath.Join(dir, "notes.txt"), "ignored\n")

	got, err := expandPattern(dir)
	if err != nil {
		t.Fatalf("expandPattern: %v", err)
	}
	if len(got) != 1 || filepath.Base(got[0]) != "models.go" {
		t.Fatalf("expandPattern = %v, want the one Go file", got)
	}
}

// TestExpandPatternIgnoresWhatTheStepDidNotProduce: a tree without sqlc must
// not fail the check over a path that was never generated.
func TestExpandPatternIgnoresWhatTheStepDidNotProduce(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "never-generated")
	got, err := expandPattern(missing)
	if err != nil {
		t.Fatalf("expandPattern: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expandPattern = %v, want nothing", got)
	}
}

// TestGenerateOutputsCoverTheGeneratedTree keeps the list honest: every
// committed artifact the generator owns is named, or `--check` cannot see it.
func TestGenerateOutputsCoverTheGeneratedTree(t *testing.T) {
	want := []string{
		"internal/db/sqlcgen",
		"web/resources/static/app.min.css",
		"docs/api-routes.md",
		"docs/unit-contracts.md",
		"*_templ.go",
	}
	for _, path := range want {
		if !slices.Contains(generateOutputs, path) {
			t.Errorf("generateOutputs is missing %q; `generate --check` cannot see it", path)
		}
	}
}
