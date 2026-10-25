package uiregistry

import (
	"path/filepath"
	"testing"
)

// Slug must mirror GitHub's anchor rules, because the registry links to doc
// headings: a mismatch turns every entry into a dangling link (and the gate
// would report anchors that a browser resolves fine).
func TestSlugMatchesGitHubAnchors(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"1. Account row (the rail's footer)":           "1-account-row-the-rails-footer",
		"2. Preference row (inside the account panel)": "2-preference-row-inside-the-account-panel",
		"5. Stack key (the demo panel's evidence)":     "5-stack-key-the-demo-panels-evidence",
		"6. State, inline":                             "6-state-inline",
		"Why these and not others":                     "why-these-and-not-others",
	}
	for heading, want := range cases {
		if got := Slug(heading); got != want {
			t.Errorf("Slug(%q) = %q, want %q", heading, got, want)
		}
	}
}

// Only numbered headings are compositions; the trailing prose section is not,
// and demanding a registry entry for it would be noise.
func TestCompositionAnchorsOnlyNumbered(t *testing.T) {
	t.Parallel()
	doc := "# UI compositions\n\n## 1. Empty state\n\n## Why these and not others\n\n## 2. Field plus its action\n"
	got := CompositionAnchors(doc)
	if len(got) != 2 || got[0] != "1-empty-state" || got[1] != "2-field-plus-its-action" {
		t.Fatalf("unexpected anchors: %v", got)
	}
}

// The committed registry must load and point at the doc it claims.
func TestCommittedRegistryIsLoadable(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	reg, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reg.Entries) == 0 {
		t.Fatal("registry is empty")
	}
	if _, ok := reg.Find("account-row"); !ok {
		t.Fatal("Find(account-row) failed")
	}
}
