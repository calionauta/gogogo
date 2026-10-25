package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The "second consumer": a foreign tree (plain HTML, its own stylesheet, no
// templ, no gogogo) that the standalone binary lints with an explicit skill.
// This is the evidence that the rules are not bound to this repo — the fixture
// is a real target, and the mutation cases below prove the fixture is live.
const (
	consumerDir = "testdata/consumer"
	repoSkill   = "../../.agents/skills/daisyui/SKILL.md"
	consumerCSS = "testdata/consumer/acme/app.css"
)

func TestConsumerTreeIsCleanWithItsOwnCSS(t *testing.T) {
	t.Parallel()
	code, out := runCLI(t, "--skill", repoSkill, "--css", consumerCSS, "--strict", consumerDir)
	if code != 0 {
		t.Fatalf("a foreign tree must lint clean, got code=%d out=%s", code, out)
	}
}

// Without --css the linter cannot know `card-grid` is the project's own class,
// and says so instead of guessing (the honest default).
func TestConsumerLocalPrefixNeedsItsCSS(t *testing.T) {
	t.Parallel()
	code, out := runCLI(t, "--skill", repoSkill, consumerDir)
	if code != 1 || !strings.Contains(out, "UNKNOWN_CLASS") || !strings.Contains(out, "card-grid") {
		t.Fatalf("expected the ambiguous local class to be reported, got code=%d out=%s", code, out)
	}
}

// copyConsumer materialises the fixture in a temp dir so a mutation cannot leak
// into the committed tree.
func copyConsumer(t *testing.T, mutation func(string) string) string {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join(consumerDir, "index.html")
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "acme"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte(mutation(string(raw))), 0o600); err != nil {
		t.Fatal(err)
	}
	css, err := os.ReadFile(filepath.Join(consumerDir, "acme", "app.css"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "acme", "app.css"), css, 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

// Each mutation is a failure mode the 2026 evidence names: a v4 relic, a
// component on the wrong element, a duplicated modifier, and design drift.
func TestConsumerMutationsAreCaught(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		mutate   func(string) string
		extra    []string
		wantCode string
	}{
		{"retired v4 class", func(s string) string {
			return strings.Replace(s, `<p class="acme-muted">`, `<p class="label-text">`, 1)
		}, nil, "RETIRED_CLASS"},
		{"toggle on an anchor", func(s string) string {
			return strings.Replace(s, `<input type="checkbox" class="toggle toggle-sm" />`,
				`<a href="#" class="toggle toggle-sm">x</a>`, 1)
		}, nil, "HOST_MISMATCH"},
		{"two sizes in one literal", func(s string) string {
			return strings.Replace(s, `class="toggle toggle-sm"`, `class="toggle toggle-sm toggle-lg"`, 1)
		}, nil, "DUPLICATE_MODIFIER"},
		{"off-scale value under --strict", func(s string) string {
			return strings.Replace(s, `class="acme-muted"`, `class="text-[13px]"`, 1)
		}, []string{"--strict"}, "ARBITRARY_VALUE"},
		{"part without its container", func(s string) string {
			return strings.Replace(s, `<section class="card bg-base-100 shadow-sm">`,
				`<section class="bg-base-100 shadow-sm">`, 1)
		}, nil, "PART_WITHOUT_PARENT"},
	}
	for _, tc := range cases {
		root := copyConsumer(t, tc.mutate)
		args := append([]string{"--skill", repoSkill, "--css", filepath.Join(root, "acme", "app.css")}, tc.extra...)
		code, out := runCLI(t, append(args, root)...)
		if code != 1 || !strings.Contains(out, tc.wantCode) {
			t.Fatalf("%s: expected %s with exit 1, got code=%d out=%s", tc.name, tc.wantCode, code, out)
		}
	}
}
