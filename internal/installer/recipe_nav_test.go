// SCOPE:plugin — a recipe that adds a sidebar entry moves the page digests, so
// the installer names the one command that re-blesses them.
package installer

import (
	"path/filepath"
	"strings"
	"testing"
)

// newNavRecipeDir writes a recipe whose declaration carries the sidebar slot.
func newNavRecipeDir(t *testing.T, nav bool) string {
	t.Helper()
	dir := t.TempDir()
	manifest := "id: pomodoro\ndeclaration:\n  routes:\n    - {method: GET, pattern: /pomodoro}\n"
	if nav {
		manifest += "  slots:\n    - nav.sidebar\n"
	}
	writeFile(t, filepath.Join(dir, recipeManifestFile), manifest)
	writeFile(t, filepath.Join(dir, "features/pomodoro/handlers/chi.go"), recipeHandlerV1)
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

	out, err = runRecipeCLI(t, "uninstall", "pomodoro", "--dir", root, "--yes")
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
