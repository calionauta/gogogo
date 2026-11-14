// The codegen half of install: pages and queries are regenerated
// so the installed recipe compiles, and their output leaves with the recipe.
package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// codegenRecipeFiles is the smallest recipe that needs both generators: a page
// (templ) and a table plus a query (sqlc).
var codegenRecipeFiles = map[string]string{
	"features/timer/handlers/chi.go":          policyRecipeHandler,
	"features/timer/components/page.templ":    "package components\n",
	"features/timer/migrations/0001_init.sql": "CREATE TABLE ext_timer_sessions (id TEXT PRIMARY KEY);\n",
	"internal/db/queries/timer.sql":           "-- name: Probe :many\nSELECT id FROM ext_timer_sessions;\n",
}

func TestRecipeInstallGeneratesItsPagesAndQueries(t *testing.T) {
	root := newRecipeCheckout(t)
	calls := stubRecipeCodegen(t)
	src := newRecipeDir(t, "v1", codegenRecipeFiles)

	out, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if got := strings.Join(*calls, ","); got != "templ,sqlc" {
		t.Fatalf("install ran %q, want templ then sqlc", got)
	}
	for _, want := range []string{"pages:    templ generated", "queries:  regenerated"} {
		if !strings.Contains(out, want) {
			t.Fatalf("install must report %q, got:\n%s", want, out)
		}
	}
}

func TestRecipeInstallSaysWhenTheQueryGeneratorIsMissing(t *testing.T) {
	root := newRecipeCheckout(t)
	stubRecipeCodegen(t)
	recipeSQLC = func(string) (bool, error) { return false, nil }
	src := newRecipeDir(t, "v1", codegenRecipeFiles)

	out, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if !strings.Contains(out, "sqlc is not on PATH: run `make sqlc`") {
		t.Fatalf("a missing generator must be named with its fix, got:\n%s", out)
	}
}

func TestRecipeUninstallRemovesTheGeneratedOutput(t *testing.T) {
	root := newRecipeCheckout(t)
	stubRecipeCodegen(t)
	src := newRecipeDir(t, "v1", codegenRecipeFiles)
	if _, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes"); err != nil {
		t.Fatal(err)
	}
	// What codegen would have written; the receipt cannot carry these (they did
	// not exist when it was computed), so uninstall derives their names.
	generated := []string{
		"features/timer/components/page_templ.go",
		"internal/db/sqlcgen/timer.sql.go",
	}
	for _, rel := range generated {
		writeFile(t, filepath.Join(root, filepath.FromSlash(rel)), "// generated\n")
	}

	out, err := runRecipeCLI(t, "uninstall", "timer", "--dir", root, "--yes")
	if err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	if !strings.Contains(out, "codegen:  removed 2 generated file(s)") {
		t.Fatalf("uninstall must report the generated files it removed, got:\n%s", out)
	}
	for _, rel := range generated {
		if _, serr := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); serr == nil {
			t.Fatalf("%s survived uninstall: the tree no longer compiles", rel)
		}
	}
}

// TestRecipeUninstallKeepsTheOutputOfAFileItKept is the counterpart rule: a file
// the user edited stays in the tree, so its generated companion must stay too —
// otherwise uninstall breaks a build it did not finish removing.
func TestRecipeUninstallKeepsTheOutputOfAFileItKept(t *testing.T) {
	root := newRecipeCheckout(t)
	stubRecipeCodegen(t)
	src := newRecipeDir(t, "v1", codegenRecipeFiles)
	if _, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes"); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(root, "features/timer/components/page.templ")
	writeFile(t, page, "package components\n// edited by the user\n")
	generated := filepath.Join(root, "features/timer/components/page_templ.go")
	writeFile(t, generated, "// generated\n")

	if _, err := runRecipeCLI(t, "uninstall", "timer", "--dir", root, "--yes"); err != nil {
		t.Fatal(err)
	}
	if _, serr := os.Stat(generated); serr != nil {
		t.Fatal("uninstall deleted the generated file of a page it kept")
	}
}
