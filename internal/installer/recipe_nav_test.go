// A recipe that adds a sidebar entry moves the page digests, so
// the installer names the one command that re-blesses them.
package installer

import (
	"path/filepath"
	"strings"
	"testing"
)

// recipeNavHandler is the code half of a sidebar contribution: the declaration
// names the slot id and the code names the constant, which is the spelling the
// receipt check has to accept.
const recipeNavHandler = "package handlers\n\nimport \"example.com/recipes/internal/recipe\"\n\n" +
	"var nav = map[recipe.SlotID][]recipe.Entry{\n" +
	"\trecipe.SlotNavSidebar: {{Label: \"Timer\", Href: \"/timer\"}},\n}\n"

// newNavRecipeDir writes a recipe whose declaration carries the sidebar slot,
// with the contribution in its code so the declaration matches what it ships.
func newNavRecipeDir(t *testing.T, nav bool) string {
	t.Helper()
	dir := t.TempDir()
	manifest := "id: timer\ndeclaration:\n  routes:\n    - {method: GET, pattern: /timer}\n"
	body := recipeHandlerV1
	if nav {
		manifest += "  slots:\n    - nav.sidebar\n"
		body = recipeNavHandler
	}
	writeFile(t, filepath.Join(dir, recipeManifestFile), manifest)
	writeFile(t, filepath.Join(dir, "features/timer/handlers/chi.go"), body)
	return dir
}

func TestRecipeWithASidebarEntryNamesTheDigestRebless(t *testing.T) {
	root := newRecipeCheckout(t)
	out, err := runRecipeCLI(t, "install", newNavRecipeDir(t, true), "--dir", root, "--yes")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if !strings.Contains(out, "PAGES_DIGEST=update") {
		t.Fatalf("a nav-contributing recipe must name the digest re-bless, got:\n%s", out)
	}

	out, err = runRecipeCLI(t, "uninstall", "timer", "--dir", root, "--yes")
	if err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	if !strings.Contains(out, "PAGES_DIGEST=update") {
		t.Fatalf("uninstall removes the entry too, so it must name the re-bless, got:\n%s", out)
	}
}

func TestRecipeWithoutASidebarEntrySaysNothingAboutDigests(t *testing.T) {
	root := newRecipeCheckout(t)
	out, err := runRecipeCLI(t, "install", newNavRecipeDir(t, false), "--dir", root, "--yes")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if strings.Contains(out, "PAGES_DIGEST") {
		t.Fatalf("a recipe with no sidebar entry cannot move the page structure, got:\n%s", out)
	}
}
