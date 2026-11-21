// Tests for the llms.txt page index: the curated agent index and the site
// manifest were two lists with nothing comparing them, so a page could be
// published for months and never be reachable from the file AGENTS.md tells an
// agent to read first. The block is generated; these tests pin what it must
// contain and that a stale block fails.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pageIndexRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cfg := `export default {
  name: "demo",
  base: "https://example.test/demo",
  repo: "owner/demo",
  manifest: [
    { group: "Start", slug: "overview", file: "overview.md", desc: "What it is." },
    { group: "Start", slug: "plugin/nested", file: "plugin/nested.md", desc: "Nested." },
  ],
}`
	write := func(rel, body string) {
		t.Helper()
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("site/docsite.config.mjs", cfg)
	write("llms.txt", "# x\n\n"+pageIndexBegin+"\n"+pageIndexEnd+"\n")
	return root
}

// TestPageIndexRegeneratesAndChecks is the RED/GREEN pair for the writer and the
// guard: an empty block must fail, the generated block must pass, and every
// published page must be named in it.
func TestPageIndexRegeneratesAndChecks(t *testing.T) {
	root := pageIndexRoot(t)
	if got := checkPageIndex(root); len(got) == 0 {
		t.Fatal("an empty page index passed the guard")
	}
	if err := writePageIndex(root); err != nil {
		t.Fatalf("writePageIndex: %v", err)
	}
	if got := checkPageIndex(root); len(got) != 0 {
		t.Fatalf("generated index still fails: %v", got)
	}
	body, err := os.ReadFile(filepath.Join(root, pageIndexFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"docs/overview.md", "docs/plugin/nested.md",
		"https://example.test/demo/docs/overview/", "**Start**",
		"https://example.test/demo/docs/plugin/nested/",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("page index is missing %q", want)
		}
	}
}

// TestPageIndexFailsOnNewPage proves the guard is not a snapshot: a page added
// to the manifest after the block was written must fail until regenerated.
// Otherwise the index would be correct once and silently stale forever.
func TestPageIndexFailsOnNewPage(t *testing.T) {
	root := pageIndexRoot(t)
	if err := writePageIndex(root); err != nil {
		t.Fatal(err)
	}
	if got := checkPageIndex(root); len(got) != 0 {
		t.Fatalf("baseline should pass: %v", got)
	}
	cfgPath := filepath.Join(root, "site/docsite.config.mjs")
	body, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	added := strings.Replace(string(body), `{ group: "Start", slug: "plugin/nested"`,
		`{ group: "Start", slug: "late", file: "late.md", desc: "Late." },
    { group: "Start", slug: "plugin/nested"`, 1)
	//nolint:gosec // G703: cfgPath is t.TempDir() plus a constant name, not input.
	if err := os.WriteFile(cfgPath, []byte(added), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := checkPageIndex(root); len(got) == 0 {
		t.Fatal("a page added after the block was written passed the guard")
	}
}
