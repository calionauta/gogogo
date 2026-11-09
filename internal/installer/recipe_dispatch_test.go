// The recipe subcommand dispatch.
package installer

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestRecipeSubcommandsDispatch pins the unified verb set: list/describe/
// invoke are recipe subcommands now (one noun: recipe, several verbs), and
// the old top-level "extension" verb is gone — no backward-compat bridge.
func TestRecipeSubcommandsDispatch(t *testing.T) {
	root := newRecipeCheckout(t)
	out, err := runRecipeCLI(t, "list", "--dir", root)
	if err != nil {
		t.Fatalf("recipe list: %v", err)
	}
	if !strings.Contains(out, "todo") {
		t.Fatalf("recipe list missing the reference recipe: %.200s", out)
	}
	if _, derr := runRecipeCLI(t, "describe", "todo", "--dir", root); derr != nil {
		t.Fatalf("recipe describe: %v", derr)
	}
	handled, herr := runSubcommand(context.Background(), []string{"extension"}, devNull(t), &bytes.Buffer{})
	if handled || herr != nil {
		t.Fatal("gogogo extension must not dispatch anymore (one concept: recipe)")
	}
}
