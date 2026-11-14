package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBarePathsRejectADeadReference is the regression for the reference that
// survived: a deleted installer file named in the agent index as plain prose.
// The backtick rule cannot see it, so this rule exists.
func TestBarePathsRejectADeadReference(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "llms.txt",
		"- Trim presets: internal/installer/trim_presets.go\n"+
			"- Live: docs/glossary.md\n")
	writeFixture(t, root, "docs/glossary.md", "# Glossary\n")

	problems := checkBarePaths(root, []string{"llms.txt"}, map[string]bool{})
	if len(problems) != 1 {
		t.Fatalf("problems = %v, want exactly the dead reference", problems)
	}
	if !contains(problems[0], "trim_presets.go") {
		t.Errorf("problem = %q, want the dead path named", problems[0])
	}
}

// TestBarePathsIgnoreProseAndCodeSpans pins the two ways this rule must stay
// quiet: a slash-joined phrase is not a path, and a backticked path is
// checkPaths' job (it carries the build-output and trimmed-tree exemptions).
func TestBarePathsIgnoreProseAndCodeSpans(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "llms.txt",
		"- `docs/` for prose, `site/index.html` for the landing\n"+
			"- Driver by target: tursogo web/desktop, ncruces gui/mobile\n"+
			"- Output only after a build: site/llms.txt\n")

	problems := checkBarePaths(root, []string{"llms.txt"}, map[string]bool{})
	if len(problems) != 0 {
		t.Fatalf("problems = %v, want none", problems)
	}
}

// TestBarePathsOnlyScanIndexPages proves the scope: a research note may name a
// path that no longer exists, because recording the past is its job.
func TestBarePathsOnlyScanIndexPages(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, filepath.Join("docs", "research", "old.md"),
		"we removed internal/installer/gone.go\n")

	problems := checkBarePaths(root, []string{filepath.Join("docs", "research", "old.md")}, map[string]bool{})
	if len(problems) != 0 {
		t.Fatalf("problems = %v, want none outside the index pages", problems)
	}
}

// contains is a local substring helper so the test reads without importing
// strings for one call.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestBareBundleDirectoryRejectsADeadBundle is the second half of the same
// regression: the dead reference was a DIRECTORY (`catalog/kiosk` here), so the
// file rule could not see it. `catalog/` and `features/` are address spaces — a
// name under them is a claim, not a phrase.
func TestBareBundleDirectoryRejectsADeadBundle(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "llms.txt",
		"- voice demo: features/kiosk\n"+
			"- missing entry: catalog/nope\n")
	if err := os.MkdirAll(filepath.Join(root, "features", "kiosk"), 0o750); err != nil {
		t.Fatal(err)
	}

	problems := checkBarePaths(root, []string{"llms.txt"}, map[string]bool{})
	if len(problems) != 1 {
		t.Fatalf("problems = %v, want only the dead bundle", problems)
	}
	if !contains(problems[0], "catalog/nope") {
		t.Errorf("problem = %q, want the dead bundle named", problems[0])
	}
}
