package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/prompts"
)

// TestLooksLikePrompt pins the heuristic in both directions. A gate that never
// fires is indistinguishable from a gate that is broken, and one that fires on
// error messages gets disabled, so both arms are asserted here.
func TestLooksLikePrompt(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "model instruction",
			text: "You are a strict code reviewer. Review the proposed fix against the original " +
				"and decide whether the proposal is an IMPROVEMENT. Reply with exactly: APPROVE. " +
				"Otherwise reply with: REVISE: <one paragraph of concrete required changes>.",
			want: true,
		},
		{
			name: "error message",
			text: "gogogo: cannot find the end of the capability registry literal; the registry " +
				"shape moved, so the scaffold refuses to guess and writes nothing.",
			want: false,
		},
		{
			name: "code template",
			text: "package handlers\n\nfunc New() *Handler { return &Handler{} }\n" +
				"// The {{name}} feature mounts itself through the generated registration.",
			want: false,
		},
		{
			name: "short instruction",
			text: "You are a copy editor. Return only the improved text.",
			want: false, // below the length floor: not a smuggled prompt surface
		},
	}
	for _, tc := range cases {
		if got := looksLikePrompt(tc.text); got != tc.want {
			t.Errorf("%s: looksLikePrompt() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestHasExemptionRequiresReason pins that a bare marker is not an escape: an
// exemption without a written reason is exactly the bypass the gate exists to
// prevent.
func TestHasExemptionRequiresReason(t *testing.T) {
	t.Parallel()
	lines := []string{
		"// allow-inline-prompt:",
		"const a = `x`",
		"// allow-inline-prompt: Go source template (codegen)",
		"const b = `y`",
	}
	if hasExemption(lines, 2) {
		t.Error("empty reason counted as an exemption")
	}
	if !hasExemption(lines, 4) {
		t.Error("reasoned exemption not honoured")
	}
}

// TestDeclaredPromptIsFound pins the rule that catches the short prompts the
// length heuristic cannot see: a `Prompt:` field holding a literal is
// model-facing by construction, whatever its length.
func TestDeclaredPromptIsFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recipe.go")
	src := `package handlers

import "github.com/calionauta/gogogo/internal/recipe"

func (p provider) Declaration() recipe.Declaration {
	return recipe.Declaration{
		Panel: &recipe.PanelSpec{
			Component: "notes_list",
			Prompt:    "notes_list {scope: all|mine} whenever the request involves the user's shared notes",
		},
	}
}
`
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	hits, errs := declaredInFile(path)
	if len(errs) > 0 {
		t.Fatalf("declaredInFile() = %v", errs)
	}
	if len(hits) != 1 {
		t.Fatalf("declaredInFile() = %d hits, want the declared prompt", len(hits))
	}
	if !strings.Contains(hits[0].Text, "notes_list") {
		t.Errorf("the hit lost the prompt text: %q", hits[0].Text)
	}
}

// TestUnusedPromptIDs pins the dead-file rule: a prompt no code loads is
// reported, and a longer id that starts with it does not count as a reference.
func TestUnusedPromptIDs(t *testing.T) {
	all := []prompts.Prompt{{ID: "panels/notes"}, {ID: "panels/notes-list"}}
	code := `package handlers

func p() string { return prompts.Text("panels/notes-list") }
`
	unused := unusedPromptIDs(code, all)
	if len(unused) != 1 || unused[0] != "panels/notes" {
		t.Fatalf("unusedPromptIDs() = %v, want only panels/notes", unused)
	}
}
