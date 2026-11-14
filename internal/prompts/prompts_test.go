package prompts

import (
	"strings"
	"testing"
)

// TestAllFilesParseAndValidate is the library-wide proof: every embedded
// .prompt file passes the same contract the gate enforces, so a broken file
// fails `go test` before it fails a request.
func TestAllFilesParseAndValidate(t *testing.T) {
	t.Parallel()
	all, err := All()
	if err != nil {
		t.Fatalf("All() = %v", err)
	}
	if len(all) == 0 {
		t.Fatal("no prompt files embedded")
	}
	ids := make(map[string]bool, len(all))
	for _, p := range all {
		if ids[p.ID] {
			t.Errorf("duplicate prompt id %q", p.ID)
		}
		ids[p.ID] = true
		if strings.TrimSpace(p.Body) == "" {
			t.Errorf("%s: empty body", p.ID)
		}
	}
}

// TestRenderSubstitutesDeclaredVariable pins the happy path the call sites use.
func TestRenderSubstitutesDeclaredVariable(t *testing.T) {
	t.Parallel()
	got, err := Render("panels/todo-list", map[string]string{"filters": "open|done"})
	if err != nil {
		t.Fatalf("Render() = %v", err)
	}
	if !strings.Contains(got, "todo_list {filter: open|done}") {
		t.Errorf("filters not substituted:\n%s", got)
	}
	if strings.Contains(got, "{{") {
		t.Errorf("unrendered placeholder left in output:\n%s", got)
	}
}

// TestRenderRejectsMissingVariable proves a half-filled prompt is refused: the
// alternative is sending a model the literal "{{partial}}".
func TestRenderRejectsMissingVariable(t *testing.T) {
	t.Parallel()
	if _, err := Render("chat/suggest-todo", nil); err == nil {
		t.Fatal("Render() with no variables = nil error, want missing-variable error")
	}
}

// TestParseRejectsContractBreaks pins the three ways a file can lie about
// itself: a name that disagrees with its path, an undeclared variable, and a
// declared variable the body never uses.
func TestParseRejectsContractBreaks(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"name mismatch": `---
name: other/name
description: "x"
version: 1
variables: []
---
Do the thing.`,
		"undeclared variable": `---
name: test/one
description: "x"
version: 1
variables: []
---
Do the {{thing}}.`,
		"unused variable": `---
name: test/one
description: "x"
version: 1
variables: [thing]
---
Do it.`,
		"no description": `---
name: test/one
version: 1
variables: []
---
Do it.`,
	}
	for name, raw := range cases {
		if _, err := Parse("test/one", []byte(raw)); err == nil {
			t.Errorf("%s: Parse() = nil error, want rejection", name)
		}
	}
}
