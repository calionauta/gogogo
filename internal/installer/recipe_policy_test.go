// The policy half of install: a recipe's routes get their rows,
// uninstall takes them away, and status names a block that went missing.
package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/recipe"
)

// policyRecipeHandler registers both routes the tests declare.
const policyRecipeHandler = "package handlers\n\n" +
	"func routes(r router) { r.Get(\"/timer\", nil); r.Post(\"/api/timer/sessions\", nil) }\n"

// newPolicyRecipeDir writes a recipe whose declaration is exactly routes.
func newPolicyRecipeDir(t *testing.T, routes ...string) string {
	t.Helper()
	dir := t.TempDir()
	var decl strings.Builder
	decl.WriteString("id: timer\ndeclaration:\n  routes:\n")
	for _, r := range routes {
		decl.WriteString("    - {method: GET, pattern: " + r + "}\n")
	}
	writeFile(t, filepath.Join(dir, recipeManifestFile), decl.String())
	writeFile(t, filepath.Join(dir, "features/timer/handlers/chi.go"), policyRecipeHandler)
	return dir
}

// policyRowsOf returns the rows inside one recipe's block.
func policyRowsOf(t *testing.T, root, id string) []string {
	t.Helper()
	lines := strings.Split(readOrEmpty(t, root, recipePolicyFile), "\n")
	_, block, found, err := splitRecipePolicy(lines, id)
	if err != nil {
		t.Fatalf("read block %s: %v", id, err)
	}
	if !found {
		return nil
	}
	var rows []string
	for _, line := range block {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			rows = append(rows, trimmed)
		}
	}
	return rows
}

func TestRecipeInstallWritesItsPolicyRows(t *testing.T) {
	root := newRecipeCheckout(t)
	// A core row already in the file must survive: the block is additive.
	writeFile(t, filepath.Join(root, filepath.FromSlash(recipePolicyFile)),
		"p, user, /todo, .*\np, user, /api/todos, .*\n")
	src := newPolicyRecipeDir(t, "/timer", "/api/timer/sessions")

	out, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if !strings.Contains(out, "policy:   2 row(s)") {
		t.Fatalf("install must report the policy rows it wrote, got:\n%s", out)
	}
	rows := policyRowsOf(t, root, "timer")
	want := []string{"p, user, /api/timer/sessions, .*", "p, user, /timer, .*"}
	if strings.Join(rows, "|") != strings.Join(want, "|") {
		t.Fatalf("block rows = %v, want %v", rows, want)
	}
	file := readOrEmpty(t, root, recipePolicyFile)
	for _, keep := range []string{"p, user, /todo, .*", "p, user, /api/todos, .*"} {
		if !strings.Contains(file, keep) {
			t.Fatalf("install dropped a row it does not own (%q):\n%s", keep, file)
		}
	}
	if got := strings.Count(file, recipePolicyBegin+"timer"); got != 1 {
		t.Fatalf("the block was written %d times, want once:\n%s", got, file)
	}
}

func TestRecipeInstallReplacesItsOwnPolicyBlockOnly(t *testing.T) {
	root := newRecipeCheckout(t)
	// Another recipe's block, with the id as a prefix of ours — the marker match
	// must be whole-line, or replacing `timer` would eat this one.
	writeFile(t, filepath.Join(root, filepath.FromSlash(recipePolicyFile)),
		"p, user, /todo, .*\n"+
			recipePolicyBegin+"timer-lab\n"+
			"p, user, /lab, .*\n"+
			recipePolicyEnd+"timer-lab\n")

	if _, err := runRecipeCLI(t, "install", newPolicyRecipeDir(t, "/timer"), "--dir", root, "--yes"); err != nil {
		t.Fatal(err)
	}
	// Re-install with a different surface: the old row must not linger.
	src := newPolicyRecipeDir(t, "/timer", "/api/timer/sessions")
	if _, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes"); err != nil {
		t.Fatal(err)
	}

	rows := policyRowsOf(t, root, "timer")
	if len(rows) != 2 {
		t.Fatalf("reinstall left %v, want the two declared rows", rows)
	}
	if got := policyRowsOf(t, root, "timer-lab"); len(got) != 1 || got[0] != "p, user, /lab, .*" {
		t.Fatalf("another recipe's block changed: %v", got)
	}
	if !strings.Contains(readOrEmpty(t, root, recipePolicyFile), "p, user, /todo, .*") {
		t.Fatal("a core row was dropped")
	}
}

func TestRecipeInstallWithoutADeclaredRouteWritesNoBlock(t *testing.T) {
	root := newRecipeCheckout(t)
	src := t.TempDir()
	writeFile(t, filepath.Join(src, recipeManifestFile), "id: notes\n")
	writeFile(t, filepath.Join(src, "features/notes/handlers/chi.go"), "package handlers\n")

	if _, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readOrEmpty(t, root, recipePolicyFile), recipePolicyBegin+"notes") {
		t.Fatal("a recipe with no routes got a policy block")
	}
}

func TestRecipeUninstallDropsItsPolicyBlock(t *testing.T) {
	root := newRecipeCheckout(t)
	if _, err := runRecipeCLI(t, "install", newPolicyRecipeDir(t, "/timer"), "--dir", root, "--yes"); err != nil {
		t.Fatal(err)
	}
	out, err := runRecipeCLI(t, "uninstall", "timer", "--dir", root, "--yes")
	if err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	if !strings.Contains(out, "policy:   dropped its block") {
		t.Fatalf("uninstall must report the dropped block, got:\n%s", out)
	}
	file := readOrEmpty(t, root, recipePolicyFile)
	if strings.Contains(file, recipePolicyBegin+"timer") {
		t.Fatalf("the block survived uninstall:\n%s", file)
	}
	if !strings.Contains(file, "p, user, /todo, .*") {
		t.Fatalf("uninstall dropped a core row:\n%s", file)
	}
}

func TestRecipeStatusReportsAMissingPolicyBlock(t *testing.T) {
	root := newRecipeCheckout(t)
	if _, err := runRecipeCLI(t, "install", newPolicyRecipeDir(t, "/timer"), "--dir", root, "--yes"); err != nil {
		t.Fatal(err)
	}
	// Someone hand-deleted the block; the routes still mount and the policy no
	// longer sees them, which is exactly the gap status has to name.
	writeFile(t, filepath.Join(root, filepath.FromSlash(recipePolicyFile)), "p, user, /todo, .*\n")

	out, err := runRecipeCLI(t, "status", "timer", "--dir", root)
	if err == nil {
		t.Fatalf("status must fail on a missing block:\n%s", out)
	}
	if !strings.Contains(out, "POLICY") || !strings.Contains(out, "no block for timer") {
		t.Fatalf("status must name the missing block, got:\n%s", out)
	}
}

func TestRecipeInstallRefusesACheckoutWithoutAPolicyFile(t *testing.T) {
	root := newRecipeCheckout(t)
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(recipePolicyFile))); err != nil {
		t.Fatal(err)
	}
	src := newPolicyRecipeDir(t, "/timer")

	_, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes")
	if err == nil || !strings.Contains(err.Error(), recipePolicyFile) {
		t.Fatalf("an install with routes and no policy file must refuse, got %v", err)
	}
	// The refusal is a preflight, so nothing of the recipe may land: no code, no
	// receipt, and no policy file conjured up to hold rows nobody can enforce.
	if _, serr := os.Stat(filepath.Join(root, "features/timer/handlers/chi.go")); serr == nil {
		t.Fatal("the refused install copied the recipe's code")
	}
	if _, serr := os.Stat(filepath.Join(root, filepath.FromSlash(recipe.ReceiptPath("timer")))); serr == nil {
		t.Fatal("the refused install wrote a receipt")
	}
	if _, serr := os.Stat(filepath.Join(root, filepath.FromSlash(recipePolicyFile))); serr == nil {
		t.Fatal("the refused install created a policy file")
	}
}
