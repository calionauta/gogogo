// The machine shape of one recipe check. Split from recipe_check.go for the
// file-size gate: the evaluator and the report are two jobs.
package installer

import "github.com/calionauta/gogogo/internal/recipe"

// RecipeCheckReport is the machine representation of one recipe check.
type RecipeCheckReport struct {
	ID            string                `json:"id"`
	Healthy       bool                  `json:"healthy"`
	Score         int                   `json:"score"`
	Errors        []string              `json:"errors,omitempty"`
	Warnings      []string              `json:"warnings,omitempty"`
	Suggestions   []string              `json:"suggestions,omitempty"`
	Complexity    recipeCheckComplexity `json:"complexity"`
	Declaration   recipe.Declaration    `json:"declaration"`
	ProviderFiles []string              `json:"providerFiles,omitempty"`
	Metrics       []checkMetricSummary  `json:"metrics,omitempty"`
	Panel         *recipe.PanelSpec     `json:"panel,omitempty"`
	// Locales is the recipe's coverage against this instance's supported
	// tags. Required is what the operator configured; Missing is what the
	// recipe does not translate.
	Locales recipeCheckLocales `json:"locales"`
	// Tests is the DERIVED inventory, never a declaration: the receipt already
	// enumerates every installed file with its hash, so the checker reads the
	// test files from there. A second list maintained by the author would be a
	// second source of truth that can only disagree with the first.
	Tests recipeCheckTests `json:"tests"`
	// Omitted is information, not a warning: the author explained why the
	// route is not headless, and the report shows that decision.
	Omitted []recipe.OmittedRoute `json:"omitted,omitempty"`
}

// recipeCheckTests is what the receipt carries as tests, plus the packages those
// tests live in. Empty Files on a recipe that owns routes or registers code is
// the one verdict this checker treats as an error rather than a warning: an
// installed slice whose behavior nobody pins is not reviewable, and it is the
// community-recipe failure mode the catalog review cannot fix from the outside.
type recipeCheckTests struct {
	Files    []string `json:"files,omitempty"`
	Packages []string `json:"packages,omitempty"`
}

// checkMetricSummary is the checker-facing shape of one declared reading. Kind
// is empty for activity; the outcome fields appear only on an outcome metric.
// One shape and one list: the report never asks a reader to merge metrics and
// outcomes.
type checkMetricSummary struct {
	Kind           string   `json:"kind,omitempty"`
	Label          string   `json:"label,omitempty"`
	Query          string   `json:"query,omitempty"`
	Signal         string   `json:"signal,omitempty"`
	Owner          *bool    `json:"owner,omitempty"`
	ID             string   `json:"id,omitempty"`
	Job            string   `json:"job,omitempty"`
	Beneficiary    string   `json:"beneficiary,omitempty"`
	Window         string   `json:"window,omitempty"`
	Success        string   `json:"success,omitempty"`
	Total          string   `json:"total,omitempty"`
	CounterMetrics []string `json:"counterMetrics,omitempty"`
}
