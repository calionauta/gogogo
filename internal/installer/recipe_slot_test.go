// The install-side half of the slot rule: a declared slot this
// build does not render is refused before a file is copied.
package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRecipeInstallRefusesASlotNothingRenders is the install-side half of the
// slot rule. Without it a manifest could declare a slot this build has no
// page for, install cleanly and produce a receipt whose content no page
// draws — the silent drop the slot contract exists to prevent, one layer
// earlier. `dashboard.card` used to be the example; its host exists now, so
// the example is an unknown slot name instead.
func TestRecipeInstallRefusesASlotNothingRenders(t *testing.T) {
	root := newRecipeCheckout(t)
	src := t.TempDir()
	writeFile(t, filepath.Join(src, recipeManifestFile),
		"id: pomodoro\n"+
			"declaration:\n  slots: [not.real]\n")
	writeFile(t, filepath.Join(src, "features/pomodoro/handlers/chi.go"), recipeHandlerV1)

	out, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes")
	if err == nil {
		t.Fatalf("a slot no page renders must be refused before anything is copied, output:\n%s", out)
	}
	if !strings.Contains(err.Error(), "not offered") || !strings.Contains(err.Error(), "not.real") {
		t.Fatalf("the refusal must name the slot and the reason, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "features/pomodoro/handlers/chi.go")); statErr == nil {
		t.Fatal("a refused install wrote a file")
	}
}
