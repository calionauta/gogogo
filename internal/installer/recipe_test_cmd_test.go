package installer

import (
	"context"
	"strings"
	"testing"
)

// TestRecipeTestSkipsARemovedReceipt pins the scope to what is INSTALLED: an
// uninstalled recipe's files are gone by design, so testing it would fail on an
// absence the user asked for. Naming it explicitly still says so.
func TestRecipeTestSkipsARemovedReceipt(t *testing.T) {
	root := newRecipeCheckout(t)
	src := newRecipeDir(t, "v1", map[string]string{
		"features/timer/handlers/chi.go":      recipeHandlerV1,
		"features/timer/handlers/chi_test.go": "package handlers\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n",
	})
	if out, err := runRecipeCLI(t, "install", src, "--dir", root, "--yes"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if out, err := runRecipeCLI(t, "uninstall", "timer", "--dir", root, "--yes"); err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	var buf strings.Builder
	err := runRecipeTest(context.Background(), []string{"--dir", root}, &buf, &buf)
	if err != nil {
		t.Fatalf("recipe test over a removed receipt must not fail: %v\n%s", err, buf.String())
	}

	// An explicit id reports the state instead of silently doing nothing.
	buf.Reset()
	if err := runRecipeTest(context.Background(), []string{"timer", "--dir", root}, &buf, &buf); err != nil {
		t.Fatalf("explicit removed id must not fail: %v", err)
	}
	if !strings.Contains(buf.String(), "uninstalled") {
		t.Errorf("explicit removed id did not report its state:\n%s", buf.String())
	}
}
