package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A cross-skill reference is the case nothing checked: the UI skill tells the
// reader to open `../gogogo-coding-standards/references/datastar.md`, and a
// rename there would leave the instruction dead in three places.
func TestCheckLinksCrossSkill(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("skills/ui/SKILL.md", "Read `../core/references/gate.md` and [the gate](../core/references/gate.md).\n")
	write("skills/core/SKILL.md", "Core.\n")
	write("skills/core/references/gate.md", "gate\n")

	skill := filepath.Join(root, "skills", "ui", "SKILL.md")
	if got := checkLinks(root, []string{skill}); len(got) != 0 {
		t.Fatalf("broken reference reported for existing files: %v", got)
	}

	// The referenced file goes away: both forms must fail.
	if err := os.Remove(filepath.Join(root, "skills", "core", "references", "gate.md")); err != nil {
		t.Fatal(err)
	}
	got := checkLinks(root, []string{skill})
	if len(got) != 2 {
		t.Fatalf("problems = %v, want the backticked path and the markdown link", got)
	}
}

func TestCheckLinksIgnoresNonReferences(t *testing.T) {
	root := t.TempDir()
	skill := filepath.Join(root, "skills", "a", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0o750); err != nil {
		t.Fatal(err)
	}
	body := "# A\n\n" +
		"See [upstream](https://example.com/x.md) and [anchor](#rules).\n" +
		"Root-relative `docs/ui.md` resolves from the repo root.\n" +
		"```\n[example](missing-example.md)\n`missing/example.md`\n```\n"
	if err := os.WriteFile(skill, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "ui.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := checkLinks(root, []string{skill}); len(got) != 0 {
		t.Fatalf("non-references reported as broken: %v", got)
	}
}

func TestSkillLinksReadsBothForms(t *testing.T) {
	links := skillLinks("a `references/x.md` and [ref](../y/z.md), not `nope.txt`.")
	joined := strings.Join(links, " ")
	for _, want := range []string{"references/x.md", "../y/z.md"} {
		if !strings.Contains(joined, want) {
			t.Errorf("links = %v, want %s", links, want)
		}
	}
	if strings.Contains(joined, "nope.txt") {
		t.Errorf("non-markdown code path collected: %v", links)
	}
}
