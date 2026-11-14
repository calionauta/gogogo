package graph

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadInstalledRecipesReadsDotRecipes(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".recipes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	yml := "id: timer\n" +
		"upstream:\n  repo: github.com/x/y\n  ref: v1\n" +
		"declaration:\n  tables:\n    - ext_timer_sessions\n"
	if err := os.WriteFile(filepath.Join(dir, "timer.yml"), []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}
	recipes, err := LoadInstalledRecipes(root)
	if err != nil {
		t.Fatalf("LoadInstalledRecipes: %v", err)
	}
	if len(recipes) != 1 || recipes[0].ID != "timer" {
		t.Fatalf("got %v, want [timer]", recipes)
	}
}
