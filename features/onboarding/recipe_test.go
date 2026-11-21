package onboarding

import (
	"testing"

	"github.com/calionauta/gogogo/internal/recipe"
)

// The id is spelled locally because this package is the engine, not a handlers
// directory — the convention's home. This pins it to the unit so a rename
// cannot leave the declaration and the capability row disagreeing.
func TestOnboardingRecipeIDMatchesTheUnit(t *testing.T) {
	var h *Handler
	if p := h.Recipe(); p != nil {
		t.Fatal("a nil handler must declare nothing (disabled engine)")
	}
	p := (&Handler{}).Recipe()
	if p == nil {
		t.Fatal("a live handler must declare a Provider")
	}
	if got := p.ID(); got != "onboarding" {
		t.Errorf("id = %q, want onboarding (the capability row's id)", got)
	}
	if problems := recipe.HeadlessCoverageProblems(p.Declaration()); len(problems) > 0 {
		t.Errorf("headless coverage: %v", problems)
	}
}
