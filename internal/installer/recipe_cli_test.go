// SCOPE:plugin — recipe CLI evidence: install writes the tree and the receipt,
// status reports what the user changed, and a blocked plan changes nothing.
package installer

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/recipe"
)

const recipeHandlerV1 = "package handlers\n\nfunc routes(r router) { r.Get(\"/pomodoro\", nil) }\n"

const recipeHandlerV2 = "package handlers\n\nfunc routes(r router) {" +
	" r.Get(\"/pomodoro\", nil); r.Get(\"/pomodoro/today\", nil) }\n"

// newRecipeCheckout builds the smallest tree looksLikeTemplate accepts.
func newRecipeCheckout(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "cmd", "web", "main.go"), "package main\n")
	return root
}

// newRecipeDir writes a recipe directory: the manifest plus the file set.
func newRecipeDir(t *testing.T, ref string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, recipeManifestFile),
		"id: pomodoro\n"+
			"upstream:\n  repo: example.com/recipes\n  ref: "+ref+"\n"+
			"declaration:\n  routes:\n    - {method: GET, pattern: /pomodoro}\n")
	for rel, body := range files {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(rel)), body)
	}
	return dir
}

// runRecipeCLI runs one recipe subcommand and returns its output and error.
func runRecipeCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	err := runRecipe(args, &buf)
	return buf.String(), err
}

func TestRecipeInstallWithoutYesChangesNothing(t *testing.T) {
	root := newRecipeCheckout(t)
	src := newRecipeDir(t, "v1", map[string]string{
		"features/pomodoro/handlers/chi.go": recipeHandlerV1,
	})

	out, err := runRecipeCLI(t, "install", src, "--dir", root)
	if err != nil {
		t.Fatalf("plan-only install must not fail: %v", err)
	}
	if !strings.Contains(out, "nothing changed") {
		t.Fatalf("expected the plan-only hint, got:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(root, "features/pomodoro/handlers/chi.go")); err == nil {
		t.Fatal("plan-only install wrote a file")
	}
	if _, err := recipe.LoadReceipt(root, "pomodoro"); err == nil {
		t.Fatal("plan-only install wrote a receipt")
	}
}

func TestRecipeInstallWritesTreeAndReceipt(t *testing.T) {
	root := newRecipeCheckout(t)
	src := newRecipeDir(t, "v1", map[string]string{
		"features/pomodoro/handlers/chi.go": recipeHandlerV1,
	})

	out, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	got, rerr := os.ReadFile(filepath.Join(root, "features/pomodoro/handlers/chi.go"))
	if rerr != nil || string(got) != recipeHandlerV1 {
		t.Fatalf("installed file = %q, %v", got, rerr)
	}
	rec, lerr := recipe.LoadReceipt(root, "pomodoro")
	if lerr != nil {
		t.Fatalf("receipt: %v", lerr)
	}
	if rec.Files["features/pomodoro/handlers/chi.go"] != recipe.Hash([]byte(recipeHandlerV1)) {
		t.Fatalf("receipt hash does not match the installed bytes: %v", rec.Files)
	}
	if len(rec.Declaration.Routes) != 1 || rec.Declaration.Routes[0].Pattern != "/pomodoro" {
		t.Fatalf("receipt lost the declaration: %+v", rec.Declaration)
	}
	if !strings.Contains(out, "the code is yours") {
		t.Fatalf("install must hand over ownership, got:\n%s", out)
	}
}

func TestRecipeStatusReportsUserEdits(t *testing.T) {
	root := newRecipeCheckout(t)
	src := newRecipeDir(t, "v1", map[string]string{
		"features/pomodoro/handlers/chi.go": recipeHandlerV1,
	})
	if _, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "features/pomodoro/handlers/chi.go"), recipeHandlerV1+"// mine\n")

	out, err := runRecipeCLI(t, "status", "--dir", root)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "1 modified") || !strings.Contains(out, "features/pomodoro/handlers/chi.go") {
		t.Fatalf("status must report the user's edit, got:\n%s", out)
	}
}

func TestRecipeInstallFastForwardsPristineFiles(t *testing.T) {
	root := newRecipeCheckout(t)
	v1 := newRecipeDir(t, "v1", map[string]string{
		"features/pomodoro/handlers/chi.go": recipeHandlerV1,
	})
	if _, err := runRecipeCLI(t, "install", v1, "--dir", root, "--yes"); err != nil {
		t.Fatal(err)
	}
	v2 := newRecipeDir(t, "v2", map[string]string{
		"features/pomodoro/handlers/chi.go": recipeHandlerV2,
	})
	if _, err := runRecipeCLI(t, "install", v2, "--dir", root, "--yes"); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "features/pomodoro/handlers/chi.go"))
	if string(got) != recipeHandlerV2 {
		t.Fatalf("pristine file was not fast-forwarded: %q", got)
	}
	rec, _ := recipe.LoadReceipt(root, "pomodoro")
	if rec.Files["features/pomodoro/handlers/chi.go"] != recipe.Hash([]byte(recipeHandlerV2)) {
		t.Fatal("receipt baseline still points at the old bytes")
	}
	if rec.Upstream.Ref != "v2" {
		t.Fatalf("receipt upstream ref = %q, want v2", rec.Upstream.Ref)
	}
}

func TestRecipeInstallRefusesAConflictAndChangesNothing(t *testing.T) {
	root := newRecipeCheckout(t)
	v1 := newRecipeDir(t, "v1", map[string]string{
		"features/pomodoro/handlers/chi.go": recipeHandlerV1,
	})
	if _, err := runRecipeCLI(t, "install", v1, "--dir", root, "--yes"); err != nil {
		t.Fatal(err)
	}
	mine := "package handlers // mine\n"
	writeFile(t, filepath.Join(root, "features/pomodoro/handlers/chi.go"), mine)
	v2 := newRecipeDir(t, "v2", map[string]string{
		"features/pomodoro/handlers/chi.go": recipeHandlerV2,
	})

	out, err := runRecipeCLI(t, "install", v2, "--dir", root, "--yes")
	if err == nil {
		t.Fatalf("a file edited by the user AND upstream must block, output:\n%s", out)
	}
	got, _ := os.ReadFile(filepath.Join(root, "features/pomodoro/handlers/chi.go"))
	if string(got) != mine {
		t.Fatalf("the user's bytes were overwritten: %q", got)
	}
	rec, _ := recipe.LoadReceipt(root, "pomodoro")
	if rec.Upstream.Ref != "v1" {
		t.Fatalf("a blocked update rewrote the receipt: ref=%q", rec.Upstream.Ref)
	}
}

func TestRecipeInstallReportsADeclarationMismatch(t *testing.T) {
	root := newRecipeCheckout(t)
	src := t.TempDir()
	writeFile(t, filepath.Join(src, recipeManifestFile),
		"id: pomodoro\n"+
			"declaration:\n  routes:\n    - {method: GET, pattern: /pomodoro}\n    - {method: GET, pattern: /pomodoro/today}\n")
	writeFile(t, filepath.Join(src, "features/pomodoro/handlers/chi.go"), recipeHandlerV1)

	out, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes")
	if err == nil {
		t.Fatalf("a declaration the code does not honour must be reported, output:\n%s", out)
	}
	if !strings.Contains(out, "/pomodoro/today") {
		t.Fatalf("the mismatch must name the missing route, got:\n%s", out)
	}
}

func TestRecipeInstallRefusesANonCheckout(t *testing.T) {
	src := newRecipeDir(t, "v1", map[string]string{
		"features/pomodoro/handlers/chi.go": recipeHandlerV1,
	})
	root := t.TempDir()
	_, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes")
	if err == nil || !strings.Contains(err.Error(), "not a gogogo checkout") {
		t.Fatalf("expected the checkout refusal, got %v", err)
	}
}
