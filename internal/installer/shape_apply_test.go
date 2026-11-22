package installer

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	shapekit "github.com/calionauta/gogogo/internal/shape"
)

// TestApplyShapePrunesEntrypointsAndChecks is the scaffold contract the CLI and
// MCP promise: `--shape web` leaves cmd/web only and writes a record the shape
// gate accepts. Without the prune the record would claim a removal that did not
// happen — exactly what `make check-shape` refuses.
func TestApplyShapePrunesEntrypointsAndChecks(t *testing.T) {
	root := t.TempDir()
	for _, ep := range shapeEntrypoints {
		dir := filepath.Join(root, filepath.FromSlash(ep))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cfg, err := applyShape(root, "web")
	if err != nil {
		t.Fatalf("applyShape: %v", err)
	}
	if cfg.Shape != shapekit.Web {
		t.Fatalf("shape = %q, want web", cfg.Shape)
	}
	for _, gone := range []string{"cmd/desktop", "cmd/gui", "cmd/mobile"} {
		if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(gone))); !os.IsNotExist(statErr) {
			t.Errorf("%s still exists after shape web", gone)
		}
	}
	if _, statErr := os.Stat(filepath.Join(root, "cmd", "web")); statErr != nil {
		t.Errorf("cmd/web was removed: %v", statErr)
	}
	problems, err := shapekit.Check(root)
	if err != nil {
		t.Fatalf("shape.Check: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("shape record disagrees with the tree: %v", problems)
	}
	loaded, err := shapekit.Load(root)
	if err != nil {
		t.Fatalf("shape.Load: %v", err)
	}
	if !loaded.Declared || loaded.Shape != shapekit.Web {
		t.Fatalf("record = %+v, want a declared web shape", loaded)
	}
}

// TestApplyShapeFullKeepsEveryProductTarget pins what `full` keeps: every
// PRODUCT target and the consumer MCP. It does NOT keep the evolution pair.
//
// This runs on the scaffold path, not on the template's own tree, so `full`
// meaning "every target" is about the product's shapes — a generated product
// must never ship `cmd/gogogo` / `cmd/gogogo-mcp`, which rewrite a source tree
// its users do not have.
func TestApplyShapeFullKeepsEveryProductTarget(t *testing.T) {
	root := t.TempDir()
	for _, ep := range shapeEntrypoints {
		dir := filepath.Join(root, filepath.FromSlash(ep))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := applyShape(root, "full")
	if err != nil {
		t.Fatalf("applyShape: %v", err)
	}

	wantRemoved := []string{"cmd/gogogo", "cmd/gogogo-mcp"}
	if !slices.Equal(cfg.Removed, wantRemoved) {
		t.Fatalf("full removed %v, want %v", cfg.Removed, wantRemoved)
	}
	for _, ep := range []string{"cmd/web", "cmd/desktop", "cmd/gui", "cmd/mobile", "cmd/product-mcp"} {
		if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(ep))); statErr != nil {
			t.Errorf("%s missing after full: %v", ep, statErr)
		}
	}
	for _, ep := range wantRemoved {
		if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(ep))); statErr == nil {
			t.Errorf("%s survived full — a product must not ship the evolution pair", ep)
		}
	}
}
