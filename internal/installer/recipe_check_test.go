// Recipe check evidence: healthy recipes pass, and a missing panel or a
// broken declaration is reported before it reaches boot or the dashboard.
package installer

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRecipeCheckHealthyConfiguration(t *testing.T) {
	root := newRecipeCheckout(t)
	src := newRecipeDir(t, "v1", map[string]string{
		"features/pomodoro/handlers/chi.go":         recipeHandlerV1,
		"features/pomodoro/handlers/register.go":    "package handlers\n\nfunc Register(reg any, d any) {}\n",
		"features/pomodoro/handlers/genui_panel.go": "package handlers\n\nconst x = \"pomodoro_list\"\n",
	}, "features/pomodoro/handlers")
	// The fixture manifest carries one metric and a panel spec.
	manifest := `id: pomodoro
upstream:
  repo: example.com/recipes
  ref: v1
declaration:
  routes:
  - {method: GET, pattern: /pomodoro}
  metrics:
  - {label: Items, query: "SELECT COUNT(*) FROM pomodoro_items WHERE owner = ?"}
  panel:
    component: "pomodoro_list"
    prompt: "pomodoro_list {scope: all|mine} whenever the user asks"
    writable: [scope]
register: features/pomodoro/handlers
`
	writeFile(t, filepath.Join(src, recipeManifestFile), manifest)
	if _, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes"); err != nil {
		t.Fatal(err)
	}

	report := checkOneRecipe(root, "pomodoro")
	if !report.Healthy {
		t.Fatalf("healthy fixture must pass, errors=%v warnings=%v output=%+v", report.Errors, report.Warnings, report)
	}
	if len(report.Metrics) != 1 || report.Panel == nil {
		t.Fatalf("checker must surface declared metrics and panel, got %+v", report)
	}
}

func TestRecipeCheckFlagsMissingPanel(t *testing.T) {
	root := newRecipeCheckout(t)
	manifest := `id: pomodoro
upstream:
  repo: example.com/recipes
  ref: v1
declaration:
  routes:
  - {method: GET, pattern: /pomodoro}
register: features/pomodoro/handlers
`
	writeFile(t, filepath.Join(root, "features", "pomodoro", "handlers", "chi.go"), recipeHandlerV1)
	writeFile(t, filepath.Join(root, "features", "pomodoro", "handlers", "register.go"), "package handlers\n")
	writeFile(t, filepath.Join(root, ".recipes", "pomodoro.yml"), manifest)
	report := checkOneRecipe(root, "pomodoro")
	if report.Healthy {
		t.Fatalf("fixture without panel or metrics must need work: %+v", report)
	}
	if !strings.Contains(strings.Join(report.Warnings, "\n"), "no genui panel declared") {
		t.Fatalf("missing-panel warning absent: %+v", report)
	}
}

// TestRecipeCheckFlagsInvisibleMutationsAndMissingActionDocs is the
// red-proof for the headless half of the evaluator: a POST route with no
// action is invisible to an agent, a verb with no description cannot be
// chosen, and an installed recipe with no summary shows only its repo path.
// All three are warnings (the recipe still boots), never silent.
func TestRecipeCheckFlagsInvisibleMutationsAndMissingActionDocs(t *testing.T) {
	root := newRecipeCheckout(t)
	code := "package handlers\n\nfunc routes(r router) { " +
		"r.Get(\"/pomodoro\", nil); r.Post(\"/pomodoro\", nil); " +
		"r.Post(\"/pomodoro/{id}/toggle\", nil) }\n"
	src := newRecipeDir(t, "v1", map[string]string{
		"features/pomodoro/handlers/chi.go":      code,
		"features/pomodoro/handlers/register.go": "package handlers\n\nfunc Register(reg any, d any) {}\n",
	}, "features/pomodoro/handlers")
	manifest := `id: pomodoro
upstream:
  repo: example.com/recipes
  ref: v1
declaration:
  routes:
  - {method: GET, pattern: /pomodoro}
  - {method: POST, pattern: /pomodoro}
  - {method: POST, pattern: "/pomodoro/{id}/toggle"}
  actions:
  - verb: show
    route: {method: GET, pattern: /pomodoro}
register: features/pomodoro/handlers
`
	writeFile(t, filepath.Join(src, recipeManifestFile), manifest)
	if out, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}

	report := checkOneRecipe(root, "pomodoro")
	if !report.Healthy {
		t.Fatalf("the gaps must be warnings, not blockers: %+v", report.Errors)
	}
	joined := strings.Join(report.Warnings, "\n")
	for _, want := range []string{
		"mutating routes with no headless action",
		"POST /pomodoro",
		"POST /pomodoro/{id}/toggle",
		"has no description",
		"no summary",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("check must report %q, got %v", want, report.Warnings)
		}
	}
}

// TestRecipeCheckHonorsDeclaredOmissions is the false-positive half: a route
// the author deliberately kept out of the headless surface must stop the
// "invisible mutation" warning and still be shown with its reason. Without
// this, every recipe with an async or demo route has a permanent warning and
// the signal for a genuinely forgotten action is lost.
func TestRecipeCheckHonorsDeclaredOmissions(t *testing.T) {
	root := newRecipeCheckout(t)
	manifest := `id: pomodoro
upstream:
  repo: example.com/recipes
  ref: v1
declaration:
  routes:
  - {method: GET, pattern: /pomodoro}
  - {method: POST, pattern: /pomodoro/suggest}
  actions:
  - verb: show
    description: Show the pomodoro page.
    route: {method: GET, pattern: /pomodoro}
  omitted:
  - route: {method: POST, pattern: /pomodoro/suggest}
    reason: asynchronous job; the result arrives through the stream
register: features/pomodoro/handlers
`
	writeFile(t, filepath.Join(root, "features", "pomodoro", "handlers", "chi.go"), recipeHandlerV1)
	writeFile(t, filepath.Join(root, "features", "pomodoro", "handlers", "register.go"), "package handlers\n")
	writeFile(t, filepath.Join(root, ".recipes", "pomodoro.yml"), manifest)
	report := checkOneRecipe(root, "pomodoro")
	joined := strings.Join(report.Warnings, "\n")
	if strings.Contains(joined, "mutating routes with no headless action") {
		t.Fatalf("an explained omission must not be reported as invisible: %v", report.Warnings)
	}
	if len(report.Omitted) != 1 || report.Omitted[0].Reason == "" {
		t.Fatalf("the report must carry the omission and its reason: %+v", report.Omitted)
	}
}

// RED/GREEN/PROVE: the score formula is the evaluator's verdict, so it must
// be pinned in both directions — a healthy recipe scores 100 minus warnings,
// and an error drops it by the documented cost. The GO GREEN half is
// TestRecipeCheckHealthyConfiguration (score 100 with zero warnings); this
// test proves the red half by hand-mutating the report and watching the
// score move.
func TestRecipeCheckScorePricesErrorsAndWarnings(t *testing.T) {
	clean := RecipeCheckReport{ID: "probe"}
	if got := recipeCheckScore(clean); got != 100 {
		t.Fatalf("clean report must score 100, got %d", got)
	}
	mixed := RecipeCheckReport{
		ID:       "probe",
		Errors:   []string{"a", "b"},
		Warnings: []string{"c"},
	}
	if got := recipeCheckScore(mixed); got != 100-20-3 {
		t.Fatalf("2 errors + 1 warning must score 77, got %d", got)
	}
	floor := RecipeCheckReport{
		ID:     "probe",
		Errors: make([]string, 0, 20),
	}
	for range 20 {
		floor.Errors = append(floor.Errors, "x")
	}
	if got := recipeCheckScore(floor); got != 0 {
		t.Fatalf("score must floor at zero, got %d", got)
	}
}

// RED/GREEN/PROVE: checkRecipeMetricsAndPanel must reject (or warn on) the
// hand-mutated shapes a drifted declaration produces, and accept the
// well-formed one. The green half is TestRecipeCheckHealthyConfiguration;
// these three mutate the fixture in the direction that must fail.
func TestRecipeCheckMetricsAndPanelRedProof(t *testing.T) {
	for _, mutate := range []struct {
		name       string
		declare    string
		wantErrors string
		wantWarns  string
	}{
		{
			name: "non-count query is an error",
			declare: `
  metrics:
  - {label: Items, query: "SELECT id FROM pomodoro_items WHERE owner = ?"}
  panel: {component: "pomodoro_list", prompt: "when", writable: [scope]}
`,
			wantErrors: "must select count",
		},
		{
			name: "non-snake-case component is an error",
			declare: `
  metrics:
  - {label: Items, query: "SELECT COUNT(*) FROM pomodoro_items WHERE owner = ?"}
  panel: {component: "PomodoroList", prompt: "when", writable: [scope]}
`,
			wantErrors: "must be snake_case",
		},
		{
			name: "duplicate metric labels are a warning",
			declare: `
  metrics:
  - {label: Items, query: "SELECT COUNT(*) FROM pomodoro_items WHERE owner = ?"}
  - {label: Items, query: "SELECT COUNT(*) FROM pomodoro_items WHERE owner = ?"}
  panel: {component: "pomodoro_list", prompt: "when", writable: [scope]}
`,
			wantWarns: "metric label",
		},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			root := newRecipeCheckout(t)
			manifest := `id: pomodoro
upstream:
  repo: example.com/recipes
  ref: v1
declaration:
  routes:
  - {method: GET, pattern: /pomodoro}
` + mutate.declare + `register: features/pomodoro/handlers
`
			writeFile(t, filepath.Join(root, "features", "pomodoro", "handlers", "chi.go"), recipeHandlerV1)
			writeFile(t, filepath.Join(root, "features", "pomodoro", "handlers", "register.go"),
				"package handlers\n\nfunc Register(reg any, d any) {}\n")
			writeFile(t, filepath.Join(root, "features", "pomodoro", "handlers", "genui_panel.go"),
				"package handlers\n\nconst x = \"pomodoro_list\"\n")
			writeFile(t, filepath.Join(root, ".recipes", "pomodoro.yml"), manifest)
			report := checkOneRecipe(root, "pomodoro")
			joined := strings.Join(report.Errors, "\n") + "\n" +
				strings.Join(report.Warnings, "\n")
			if mutate.wantErrors != "" && !strings.Contains(joined, mutate.wantErrors) {
				t.Fatalf("mutated declaration must surface %q, got errors=%v warnings=%v",
					mutate.wantErrors, report.Errors, report.Warnings)
			}
			if mutate.wantWarns != "" && !strings.Contains(joined, mutate.wantWarns) {
				t.Fatalf("mutated declaration must surface warning %q, got errors=%v warnings=%v",
					mutate.wantWarns, report.Errors, report.Warnings)
			}
			if !report.Healthy && mutate.wantErrors != "" {
				return
			}
			// Duplicate-label is a warning by design: healthy stays true
			// but the warning must be present and the score below 100.
			if mutate.wantWarns != "" && report.Score >= 100 {
				t.Fatalf("warning must lower the score below 100, got %d", report.Score)
			}
		})
	}
}
