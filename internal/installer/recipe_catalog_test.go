// SCOPE:plugin — recipe install from a reviewed catalog entry.
package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/recipe"
)

// newCatalogFixture writes a content directory plus a reviewed entry whose hashes
// match it. Mutating either after this call is how the refusal tests are built.
func newCatalogFixture(t *testing.T, files map[string]string) (entryPath, contentsDir string) {
	t.Helper()
	contentsDir = t.TempDir()
	paths := make([]string, 0, len(files))
	for rel, body := range files {
		writeFile(t, filepath.Join(contentsDir, filepath.FromSlash(rel)), body)
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	var b strings.Builder
	b.WriteString("id: pomodoro\nversion: v1.2.0\n")
	b.WriteString("source:\n  repo: example.com/recipes\n  ref: v1.0.0\n")
	b.WriteString("module: example.com/recipes\n")
	b.WriteString("register: features/pomodoro/handlers\n")
	b.WriteString("files:\n")
	for _, rel := range paths {
		fmt.Fprintf(&b, "  %s: sha256:%s\n", rel, recipe.Hash([]byte(files[rel])))
	}
	b.WriteString("license: MIT\nminCore: v0.56.0\n")
	b.WriteString("slots: [settings.section]\n")
	b.WriteString("routes:\n  - {method: GET, pattern: /pomodoro}\n")
	b.WriteString("upstreamDeps: []\n")
	b.WriteString("reviewedAt: \"2026-11-14\"\nreviewedBy: calionauta\nreviewedAgainst: 3f8a1c2\n")
	entryPath = filepath.Join(t.TempDir(), "entry.yml")
	writeFile(t, entryPath, b.String())
	return entryPath, contentsDir
}

// catalogFiles is the reviewed file set. The register package contributes the
// slot the entry declares: a declaration with no contribution is what the
// receipt check reports, so a fixture without it would be an install that cannot
// exist.
func catalogFiles() map[string]string {
	return map[string]string{
		"features/pomodoro/handlers/chi.go": recipeHandlerV1,
		"features/pomodoro/handlers/register.go": "package handlers\n\n" +
			"import \"example.com/recipes/internal/recipe\"\n\n" +
			"var slots = map[recipe.SlotID]recipe.Fragment{\n" +
			"\trecipe.SlotSettingsSection: {Title: \"Pomodoro\"},\n}\n\n" +
			"func Register(reg any, d any) {}\n",
	}
}

func TestRecipeInstallFromCatalogEntryVerifiesAndInstalls(t *testing.T) {
	root := newRecipeCheckout(t)
	entry, contents := newCatalogFixture(t, catalogFiles())

	out, err := runRecipeCLI(t, "install", "--entry", entry, contents, "--dir", root, "--yes")
	if err != nil {
		t.Fatalf("install from entry: %v\n%s", err, out)
	}
	got, rerr := os.ReadFile(filepath.Join(root, "features/pomodoro/handlers/chi.go"))
	if rerr != nil || string(got) != recipeHandlerV1 {
		t.Fatalf("reviewed bytes were not installed: %q, %v", got, rerr)
	}
	rec, lerr := recipe.LoadReceipt(root, "pomodoro")
	if lerr != nil {
		t.Fatalf("receipt: %v", lerr)
	}
	if rec.Upstream.Ref != "v1.0.0" {
		t.Fatalf("receipt upstream = %+v, want the entry's source", rec.Upstream)
	}
	if len(rec.Declaration.Routes) != 1 || rec.Declaration.Routes[0].Pattern != "/pomodoro" {
		t.Fatalf("receipt lost the entry's declaration: %+v", rec.Declaration)
	}
	if len(rec.Declaration.Slots) != 1 || rec.Declaration.Slots[0] != "settings.section" {
		t.Fatalf("receipt lost the declared slot: %+v", rec.Declaration.Slots)
	}
	if gen := readOrEmpty(t, root, recipeRegistrationFile); !strings.Contains(gen, "pomodoro.Register") {
		t.Fatalf("entry install did not register the recipe:\n%s", gen)
	}
}

func TestRecipeInstallFromCatalogEntryRejectsTamperedBytes(t *testing.T) {
	root := newRecipeCheckout(t)
	entry, contents := newCatalogFixture(t, catalogFiles())
	writeFile(t, filepath.Join(contents, "features/pomodoro/handlers/chi.go"), "package handlers // tampered\n")

	out, err := runRecipeCLI(t, "install", "--entry", entry, contents, "--dir", root, "--yes")
	if err == nil || !strings.Contains(err.Error(), "does not hash to the reviewed bytes") {
		t.Fatalf("tampered bytes must be refused, got %v\n%s", err, out)
	}
	if _, serr := os.Stat(filepath.Join(root, "features/pomodoro/handlers/chi.go")); !os.IsNotExist(serr) {
		t.Fatal("a refused entry install wrote files")
	}
}

func TestRecipeInstallFromCatalogEntryRejectsUnreviewedFiles(t *testing.T) {
	root := newRecipeCheckout(t)
	entry, contents := newCatalogFixture(t, catalogFiles())
	writeFile(t, filepath.Join(contents, "features/pomodoro/backdoor.go"), "package pomodoro\n")

	out, err := runRecipeCLI(t, "install", "--entry", entry, contents, "--dir", root, "--yes")
	if err == nil || !strings.Contains(err.Error(), "did not review") {
		t.Fatalf("an unreviewed file must be refused, got %v\n%s", err, out)
	}
}
