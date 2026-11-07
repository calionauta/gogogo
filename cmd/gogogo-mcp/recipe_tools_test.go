package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecipeToolsRequireDirAndConfirm(t *testing.T) {
	for _, tt := range []struct {
		name string
		call func() error
	}{
		{"install", func() error {
			_, _, err := handleRecipeInstall(context.Background(), nil, recipeArgs{Src: "/tmp/recipe"})
			return err
		}},
		{"update", func() error {
			_, _, err := handleRecipeUpdate(context.Background(), nil, recipeArgs{Src: "/tmp/recipe"})
			return err
		}},
		{"uninstall", func() error {
			_, _, err := handleRecipeUninstall(context.Background(), nil, recipeArgs{ID: "pomodoro"})
			return err
		}},
		{"status", func() error { _, _, err := handleRecipeStatus(context.Background(), nil, recipeArgs{}); return err }},
	} {
		if err := tt.call(); err == nil || !strings.Contains(err.Error(), "required") {
			t.Errorf("%s without dir: expected required-fields refusal, got %v", tt.name, err)
		}
	}
}

func TestRecipeToolsDryRunReachesTheEngine(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "cmd", "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/fixture\n\ngo 1.27\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal", "rbac", "casbin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "rbac", "casbin", "policy.csv"), []byte("p, admin, /*, .*\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmd", "web", "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const manifest = "id: pomodoro\ndeclaration:\n  routes:\n    - {method: GET, pattern: /pomodoro}\n"
	if err := os.WriteFile(filepath.Join(root, ".recipe.yml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	res, _, err := handleRecipeInstall(context.Background(), nil, recipeArgs{
		Src: root, Dir: root, DryRun: true, Format: "json",
	})
	if err != nil {
		t.Fatalf("handleRecipeInstall: %v", err)
	}
	if text := toolText(t, res); !strings.Contains(text, "pomodoro") {
		t.Errorf("plan does not name the recipe: %s", text)
	}
	if _, statErr := os.Stat(filepath.Join(root, "features")); !os.IsNotExist(statErr) {
		t.Error("dry run copied files — it must change nothing")
	}
	_, _, err = handleRecipeUpdate(context.Background(), nil, recipeArgs{
		Src: root, Dir: root, DryRun: true, Format: "json",
	})
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("update without a receipt must refuse, got %v", err)
	}
}

func TestRecipeStatusReadsAnEmptyCheckout(t *testing.T) {
	dir := t.TempDir()
	// Smallest template checkout `looksLikeTemplate` accepts: the two
	// marker files and an empty receipts dir. No receipt means "nothing
	// installed yet", which is a success answer, not a refusal.
	for _, f := range []struct{ path, body string }{
		{"cmd/web/main.go", "package main\n"},
		{"internal/installer/run.go", "package installer\n"},
		{"go.mod", "module example.com/demo\n\ngo 1.27\n"},
	} {
		full := filepath.Join(dir, filepath.FromSlash(f.path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(f.body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, ".recipes"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, _, err := handleRecipeStatus(context.Background(), nil, recipeArgs{
		Dir: dir, Format: "json",
	})
	if err != nil {
		t.Fatalf("handleRecipeStatus: %v", err)
	}
	if text := toolText(t, res); !strings.Contains(text, "nothing is installed yet") {
		t.Errorf("empty checkout status changed meaning: %s", text)
	}
}
