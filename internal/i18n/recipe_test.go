// Recipe catalogs: the merge, the prefix, the collision, the coverage report.
package i18n

import (
	"strings"
	"testing"
	"testing/fstest"
)

// recipeFS builds a recipe's locale files in memory.
func recipeFS(files map[string]string) fstest.MapFS {
	out := fstest.MapFS{}
	for path, body := range files {
		out[path] = &fstest.MapFile{Data: []byte(body)}
	}
	return out
}

// TestRecipeStringsAreMergedUnderTheirPrefix is the seam's promise: a recipe
// says `title`, the catalog answers `<id>.title`, and the core keys still work.
func TestRecipeStringsAreMergedUnderTheirPrefix(t *testing.T) {
	core := Default()
	merged, err := core.WithRecipes([]RecipeCatalog{{
		ID: "kiosk",
		FS: recipeFS(map[string]string{
			"locale/en.json":    `{"title":"Kiosk","confirm":"Confirm"}`,
			"locale/pt-BR.json": `{"title":"Quiosque","confirm":"Confirmar"}`,
		}),
	}})
	if err != nil {
		t.Fatalf("WithRecipes: %v", err)
	}
	for _, tc := range []struct{ tag, key, want string }{
		{"en", "kiosk.title", "Kiosk"},
		{"pt-BR", "kiosk.title", "Quiosque"},
		{"pt-BR", "kiosk.confirm", "Confirmar"},
		// Core strings are untouched by the merge.
		{"pt-BR", "todo.tasks", "Tarefas"},
	} {
		got, ok := merged.Lookup(tc.tag, tc.key)
		if !ok || got != tc.want {
			t.Errorf("Lookup(%s, %s) = %q,%v want %q", tc.tag, tc.key, got, ok, tc.want)
		}
	}
	// The receiver is immutable: the boot catalog must not have gained the
	// recipe's keys.
	if _, ok := core.Lookup("en", "kiosk.title"); ok {
		t.Error("the merge mutated the boot catalog")
	}
}

// TestTwoRecipesCannotCollide pins the prefix's purpose: two recipes with the
// same local key coexist, and a duplicate id is refused.
func TestTwoRecipesCannotCollide(t *testing.T) {
	files := map[string]string{"locale/en.json": `{"title":"X"}`}
	merged, err := Default().WithRecipes([]RecipeCatalog{
		{ID: "a", FS: recipeFS(files)},
		{ID: "b", FS: recipeFS(files)},
	})
	if err != nil {
		t.Fatalf("two recipes with the same local key must coexist: %v", err)
	}
	for _, id := range []string{"a", "b"} {
		if _, ok := merged.Lookup("en", id+".title"); !ok {
			t.Errorf("%s.title missing", id)
		}
	}

	_, err = Default().WithRecipes([]RecipeCatalog{
		{ID: "a", FS: recipeFS(files)},
		{ID: "a", FS: recipeFS(files)},
	})
	if err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("a duplicate recipe id must be refused, got %v", err)
	}
}

// TestRecipeWithNoCatalogIsAnError: a Translator that answers nil is skipped by
// the registry, so reaching the merge with an empty FS is a bug, not a no-op.
func TestRecipeWithNoCatalogIsAnError(t *testing.T) {
	_, err := Default().WithRecipes([]RecipeCatalog{{
		ID: "empty", FS: recipeFS(map[string]string{}),
	}})
	if err == nil || !strings.Contains(err.Error(), "no catalog") {
		t.Fatalf("a recipe with no locale files must be refused, got %v", err)
	}
}

// TestPartialRecipeCatalogFallsBack: a recipe shipping only English still
// answers pt-BR, through the core fallback.
func TestPartialRecipeCatalogFallsBack(t *testing.T) {
	merged, err := Default().WithRecipes([]RecipeCatalog{{
		ID: "half", FS: recipeFS(map[string]string{"locale/en.json": `{"title":"Half"}`}),
	}})
	if err != nil {
		t.Fatalf("WithRecipes: %v", err)
	}
	if got, ok := merged.Lookup("pt-BR", "half.title"); !ok || got != "Half" {
		t.Errorf("a partial catalog must fall back, got %q,%v", got, ok)
	}
}

// TestCoverageReportsTheGap is the advisory half: the merge works, and the
// missing locale is still reported so it can be fixed.
func TestCoverageReportsTheGap(t *testing.T) {
	gaps := CheckRecipeCoverage([]string{"en", "pt-BR"}, []RecipeCatalog{{
		ID: "half", FS: recipeFS(map[string]string{"locale/en.json": `{"title":"Half"}`}),
	}})
	if len(gaps) != 1 || !strings.Contains(gaps[0], "pt-BR") {
		t.Fatalf("expected one pt-BR gap, got %v", gaps)
	}
	if gaps := CheckRecipeCoverage([]string{"en", "pt-BR"}, []RecipeCatalog{{
		ID: "full", FS: recipeFS(map[string]string{
			"locale/en.json": `{"t":"a"}`, "locale/pt-BR.json": `{"t":"b"}`,
		}),
	}}); len(gaps) != 0 {
		t.Fatalf("a complete recipe must report nothing, got %v", gaps)
	}
}
