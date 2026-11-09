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
