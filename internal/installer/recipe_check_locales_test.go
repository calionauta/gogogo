// Locale coverage: a recipe that ships translations must cover the instance's
// supported tags; a recipe with none is a warning unless the operator asked
// for strict coverage.
package installer

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/projectlocales"
	"github.com/calionauta/gogogo/internal/recipe"
)

func TestLocaleCoverageGradesPartialAndMissingCatalogs(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("features/timer/locale/en.json", `{"title":"Timer"}`)
	receipt := recipe.Receipt{ID: "timer", Files: map[string]string{
		"features/timer/locale/en.json": "hash",
	}}

	coverage := checkRecipeLocales(root, receipt, []string{"en", "pt-BR"})
	if len(coverage.Shipped) != 1 || coverage.Shipped[0] != "en" {
		t.Fatalf("shipped = %v, want [en]", coverage.Shipped)
	}
	errs, warns := localeProblems("timer", coverage, false)
	if len(errs) != 1 || !strings.Contains(errs[0], "misses pt-BR") {
		t.Fatalf("partial catalog must be an error, got errors=%v warnings=%v", errs, warns)
	}

	empty := recipe.Receipt{ID: "empty"}
	coverage = checkRecipeLocales(root, empty, []string{"en"})
	errs, warns = localeProblems("empty", coverage, false)
	if len(errs) != 0 || len(warns) != 1 {
		t.Fatalf("no catalog must be a warning, got errors=%v warnings=%v", errs, warns)
	}
	errs, _ = localeProblems("empty", coverage, true)
	if len(errs) != 1 {
		t.Fatalf("strict coverage must fail a missing catalog, got %v", errs)
	}
}

func TestLocaleCoverageIsCleanWhenEveryTagShips(t *testing.T) {
	root := t.TempDir()
	for _, tag := range []string{"en", "pt-BR"} {
		full := filepath.Join(root, "features", "timer", "locale", tag+".json")
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(`{"title":"Timer"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	receipt := recipe.Receipt{ID: "timer", Files: map[string]string{
		"features/timer/locale/en.json":    "hash",
		"features/timer/locale/pt-BR.json": "hash",
	}}
	coverage := checkRecipeLocales(root, receipt, []string{"en", "pt-BR"})
	if errs, warns := localeProblems("timer", coverage, false); len(errs)+len(warns) != 0 {
		t.Fatalf("a full catalog must be clean, got errors=%v warnings=%v", errs, warns)
	}
}

// TestPartialDeclarationTranslationsBlock: a recipe that translates its metric
// labels for one shipped language but not the other is a mixed-language grid,
// which is exactly the failure the operator's language set exists to prevent.
func TestPartialDeclarationTranslationsBlock(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "features/demo/locale/en.json"), "{}\n")
	writeFile(t, filepath.Join(root, "features/demo/locale/pt-BR.json"), "{}\n")
	r := recipe.Receipt{
		ID:    "demo",
		Files: map[string]string{"features/demo/locale/en.json": "", "features/demo/locale/pt-BR.json": ""},
		Declaration: recipe.Declaration{
			Metrics: []recipe.Metric{{Kind: recipe.MetricActivity, Label: "Orders", Query: "SELECT COUNT(*) FROM t"}},
			Translations: []recipe.Translations{
				{Locale: "pt-BR", Metrics: map[string]string{"Orders": "Pedidos"}},
			},
		},
	}
	got := checkRecipeLocales(root, r, []string{"en", "pt-BR"})
	if len(got.MissingDeclared) != 0 {
		t.Fatalf("en must not be required in translations: %v", got.MissingDeclared)
	}
	got = checkRecipeLocales(root, r, []string{"en", "pt-BR", "es"})
	if len(got.MissingDeclared) != 1 || got.MissingDeclared[0] != "es" {
		t.Fatalf("es should be missing from the declaration: %v", got.MissingDeclared)
	}
	if errs, _ := localeProblems("demo", got, false); len(errs) == 0 {
		t.Fatal("a partially translated declaration was accepted")
	}
}

// TestCheckLocalesFollowsTheDirFlag: the tags come from the tree being graded,
// so `--dir other` must read OTHER's record, not the current directory's.
func TestCheckLocalesFollowsTheDirFlag(t *testing.T) {
	root := t.TempDir()
	if _, err := projectlocales.Write(root, []string{"en", "pt-BR", "es"}); err != nil {
		t.Fatal(err)
	}
	opt, err := parseRecipeLookupArgs("check", []string{"--dir", root})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !slices.Contains(opt.locales, "es") {
		t.Fatalf("locales = %v; the --dir record was not read", opt.locales)
	}
}
