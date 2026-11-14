// The recipe's own declaration, held against every gate the engine applies.
//
// This is the test that keeps the Go declaration and the YAML honest: install
// compares them, and this catches a defect before an install is attempted. It
// builds no handler — the declaration reads no dependency.
package handlers

import (
	"testing"

	"github.com/calionauta/gogogo/internal/recipe"
)

func TestDeclarationPassesEveryGate(t *testing.T) {
	d := recipeProvider{}.Declaration()
	gates := []struct {
		name     string
		problems []string
	}{
		{"metrics", recipe.MetricProblems(d)},
		{"panel", recipe.PanelProblems(d)},
		{"outcomes", recipe.OutcomeTableProblems(d)},
		{"actions", recipe.ActionProblems(d)},
		{"action docs", recipe.ActionDocProblems(d)},
		{"omissions", recipe.OmissionProblems(d)},
	}
	for _, gate := range gates {
		if len(gate.problems) > 0 {
			t.Errorf("%s: %v", gate.name, gate.problems)
		}
	}
}

// TestDeclarationOwesItsOutcomeToItsOwnTable pins the outcome's one enforceable
// claim: the ratio reads rows the recipe owns, not a core table and not a
// table it forgot to declare.
func TestDeclarationOwesItsOutcomeToItsOwnTable(t *testing.T) {
	d := recipeProvider{}.Declaration()
	if len(d.Outcomes()) != 1 {
		t.Fatalf("want exactly one declared outcome, got %d", len(d.Outcomes()))
	}
	// Dropping the table must break the outcome — the positive case above would
	// pass a declaration that never checked ownership.
	d.Tables = nil
	problems := recipe.OutcomeTableProblems(d)
	if len(problems) == 0 {
		t.Fatal("an outcome passed with no declared table")
	}
}
