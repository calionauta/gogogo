// SCOPE:plugin — recipe CLI evidence: install writes the tree and the receipt,
// status reports what the user changed, and a blocked plan changes nothing.
package installer

import (
	"bytes"
	"encoding/json"
	"go/format"
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
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.26\n")
	return root
}

// newRecipeDir writes a recipe directory: the manifest plus the file set.
func newRecipeDir(t *testing.T, ref string, files map[string]string, register ...string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := "id: pomodoro\n" +
		"upstream:\n  repo: example.com/recipes\n  ref: " + ref + "\n" +
		"declaration:\n  routes:\n    - {method: GET, pattern: /pomodoro}\n"
	if len(register) > 0 && register[0] != "" {
		manifest += "register: " + register[0] + "\n"
	}
	writeFile(t, filepath.Join(dir, recipeManifestFile), manifest)
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

func readOrEmpty(t *testing.T, root, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return ""
	}
	return string(raw)
}

// recipeWithRegister is a recipe whose copied package exports the seam contract.
func recipeWithRegister(t *testing.T) string {
	t.Helper()
	return newRecipeDir(t, "v1", map[string]string{
		"features/pomodoro/handlers/chi.go":      recipeHandlerV1,
		"features/pomodoro/handlers/register.go": "package handlers\n\nfunc Register(reg any, d any) {}\n",
	}, "features/pomodoro/handlers")
}

func TestRecipeInstallWritesRegistration(t *testing.T) {
	root := newRecipeCheckout(t)
	src := recipeWithRegister(t)

	if out, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	gen := readOrEmpty(t, root, recipeRegistrationFile)
	if !strings.Contains(gen, `"example.com/app/features/pomodoro/handlers"`) {
		t.Fatalf("generated registration does not import the recipe under the tree's module:\n%s", gen)
	}
	if !strings.Contains(gen, "pomodoro.Register,") {
		t.Fatalf("generated registration does not call the recipe:\n%s", gen)
	}
	if formatted, ferr := format.Source([]byte(gen)); ferr != nil || string(formatted) != gen {
		t.Fatalf("generated registration is not gofmt-clean: %v\n%s", ferr, gen)
	}
}

func TestRecipeUninstallMarksRemovedAndUnregisters(t *testing.T) {
	root := newRecipeCheckout(t)
	src := recipeWithRegister(t)
	if _, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes"); err != nil {
		t.Fatal(err)
	}

	out, err := runRecipeCLI(t, "uninstall", "pomodoro", "--dir", root, "--yes")
	if err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	if _, serr := os.Stat(filepath.Join(root, "features/pomodoro/handlers/chi.go")); !os.IsNotExist(serr) {
		t.Fatal("a pristine file survived uninstall")
	}
	rec, lerr := recipe.LoadReceipt(root, "pomodoro")
	if lerr != nil {
		t.Fatalf("receipt must stay after uninstall: %v", lerr)
	}
	if rec.Removed == "" {
		t.Fatal("receipt was not marked removed")
	}
	gen := readOrEmpty(t, root, recipeRegistrationFile)
	if strings.Contains(gen, "pomodoro.Register") {
		t.Fatalf("registration still imports a removed recipe:\n%s", gen)
	}
	if !strings.Contains(gen, "[]register.Func{}") {
		t.Fatalf("registration is not the empty set:\n%s", gen)
	}
}

func TestRecipeUninstallKeepsEditedFiles(t *testing.T) {
	root := newRecipeCheckout(t)
	src := recipeWithRegister(t)
	if _, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes"); err != nil {
		t.Fatal(err)
	}
	mine := recipeHandlerV1 + "// mine\n"
	writeFile(t, filepath.Join(root, "features/pomodoro/handlers/chi.go"), mine)

	if out, err := runRecipeCLI(t, "uninstall", "pomodoro", "--dir", root, "--yes"); err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	got, rerr := os.ReadFile(filepath.Join(root, "features/pomodoro/handlers/chi.go"))
	if rerr != nil || string(got) != mine {
		t.Fatalf("uninstall destroyed the user's bytes: %q, %v", got, rerr)
	}
}

func TestRecipeInstallRejectsAnUnsafeRegisterPath(t *testing.T) {
	root := newRecipeCheckout(t)
	src := newRecipeDir(t, "v1", map[string]string{
		"evil/handler.go": "package evil\n",
	}, "../evil")
	_, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes")
	if err == nil || !strings.Contains(err.Error(), "clean repo-relative") {
		t.Fatalf("expected the unsafe-path refusal, got %v", err)
	}
	if _, serr := os.Stat(filepath.Join(root, "evil/handler.go")); !os.IsNotExist(serr) {
		t.Fatal("a refused install wrote files")
	}
}

func TestRecipeInstallRejectsAMissingRegisterPackage(t *testing.T) {
	root := newRecipeCheckout(t)
	src := newRecipeDir(t, "v1", map[string]string{
		"features/pomodoro/handlers/chi.go": recipeHandlerV1,
	}, "features/pomodoro/other")
	_, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes")
	if err == nil || !strings.Contains(err.Error(), "ships no files under it") {
		t.Fatalf("expected the missing-package refusal, got %v", err)
	}
}

func TestRecipeStatusShowsARemovedReceipt(t *testing.T) {
	root := newRecipeCheckout(t)
	src := recipeWithRegister(t)
	if _, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes"); err != nil {
		t.Fatal(err)
	}
	if _, err := runRecipeCLI(t, "uninstall", "pomodoro", "--dir", root, "--yes"); err != nil {
		t.Fatal(err)
	}
	out, err := runRecipeCLI(t, "status", "--dir", root)
	if err != nil {
		t.Fatalf("status of a removed receipt: %v", err)
	}
	if !strings.Contains(out, "removed") || !strings.Contains(out, "data ownership") {
		t.Fatalf("status must report the removal, got:\n%s", out)
	}
}

func TestRecipeReinstallRestoresFiles(t *testing.T) {
	root := newRecipeCheckout(t)
	src := recipeWithRegister(t)
	if _, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes"); err != nil {
		t.Fatal(err)
	}
	if _, err := runRecipeCLI(t, "uninstall", "pomodoro", "--dir", root, "--yes"); err != nil {
		t.Fatal(err)
	}
	out, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes")
	if err != nil {
		t.Fatalf("reinstall: %v\n%s", err, out)
	}
	if _, serr := os.Stat(filepath.Join(root, "features/pomodoro/handlers/chi.go")); serr != nil {
		t.Fatalf("reinstall did not restore the deleted file: %v", serr)
	}
	rec, lerr := recipe.LoadReceipt(root, "pomodoro")
	if lerr != nil || rec.Removed != "" {
		t.Fatalf("reinstall left the receipt removed: %+v, %v", rec, lerr)
	}
	if gen := readOrEmpty(t, root, recipeRegistrationFile); !strings.Contains(gen, "pomodoro.Register") {
		t.Fatalf("reinstall did not register the recipe again:\n%s", gen)
	}
}

// TestRepoRegistrationIsFresh is the drift gate for the generated file the repo
// commits: it must equal what the current receipts render, exactly as the CSS
// and templ gates compare their artifacts against HEAD.
func TestRepoRegistrationIsFresh(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	want, cerr := registrationContent(root)
	if cerr != nil {
		t.Fatalf("render registration: %v", cerr)
	}
	got, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(recipeRegistrationFile)))
	if rerr != nil {
		t.Fatalf("read %s: %v (run a recipe install once)", recipeRegistrationFile, rerr)
	}
	if string(got) != string(want) {
		t.Fatalf("%s is stale — regenerate it from the receipts:\n--- have ---\n%s\n--- want ---\n%s",
			recipeRegistrationFile, got, want)
	}
}

func TestRecipeInstallJSON(t *testing.T) {
	root := newRecipeCheckout(t)
	src := recipeWithRegister(t)
	out, err := runRecipeCLI(t, "install", src, "--dir", root, "--format", "json")
	if err != nil {
		t.Fatalf("plan-only json: %v\n%s", err, out)
	}
	var rep recipeInstallReport
	if uerr := json.Unmarshal([]byte(out), &rep); uerr != nil {
		t.Fatalf("install --format json is not JSON: %v\n%s", uerr, out)
	}
	if rep.Applied || rep.Write != 2 || rep.ID != "pomodoro" {
		t.Fatalf("unexpected report: %+v", rep)
	}
}

func TestRecipeUpdateRequiresAnInstalledReceipt(t *testing.T) {
	root := newRecipeCheckout(t)
	src := recipeWithRegister(t)
	_, err := runRecipeCLI(t, "update", src, "--dir", root, "--yes")
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("update without a receipt must refuse, got %v", err)
	}
}
