package main

import (
	"strings"
	"testing"
)

// TestRetiredTermsAreRejected pins the vocabulary gate: a retired compound
// form is reported, the word that replaced it is named, and the pages that
// may mention the past (a dated evaluation, the glossary itself) are exempt.
func TestRetiredTermsAreRejected(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "docs/page.md", "Run `gogogo extension install` to add one.\n")
	writeFixture(t, root, "docs/other.md", "Run `gogogo recipe install` to add one.\n")

	problems := checkRetiredTerms(root, []string{"docs/page.md", "docs/other.md"})
	if len(problems) != 1 {
		t.Fatalf("expected one retired name, got %v", problems)
	}
	if !strings.Contains(problems[0], "gogogo recipe") {
		t.Errorf("the replacement is not named: %v", problems[0])
	}
}

// TestRetiredTermsExemptThePast pins the exemptions: a dated evaluation
// records the past, and the glossary DEFINES the retired names.
func TestRetiredTermsExemptThePast(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "docs/research/old.md", "`gogogo extension list` was the old verb.\n")
	writeFixture(t, root, glossaryPage, "`gogogo extension list` is retired.\n")

	problems := checkRetiredTerms(root, []string{"docs/research/old.md", glossaryPage})
	if len(problems) != 0 {
		t.Fatalf("the past may be named in a research note and in the glossary: %v", problems)
	}
}
