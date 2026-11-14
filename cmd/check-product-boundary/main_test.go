package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree lays down a minimal checkout: four Go files whose imports the gate
// reads. Only the import lines matter — the gate parses, it does not build.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestProductMustNotImportEvolutionTools is the tripwire's reason to exist: the
// product binary has to build after `cmd/gogogo` is deleted, so one import in
// cmd/web turns the template back into a framework.
func TestProductMustNotImportEvolutionTools(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"cmd/web/main.go": "package main\n\nimport (\n\t_ \"github.com/calionauta/gogogo/internal/" +
			"installer\"\n)\n\nfunc main() {}\n",
		"internal/server/server.go":       "package server\n",
		"internal/installer/installer.go": "package installer\n",
	})

	problems, err := check(root)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(problems) != 1 {
		t.Fatalf("problems = %v, want exactly the cmd/web import", problems)
	}
	if !strings.Contains(problems[0], "cmd/web imports") {
		t.Errorf("problem = %q, want it to name cmd/web", problems[0])
	}
}

// TestInstallerMustNotLinkTheServer keeps the read/write distinction: the
// installer edits a checkout's source, it does not import the running product.
func TestInstallerMustNotLinkTheServer(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"cmd/web/main.go":           "package main\n\nfunc main() {}\n",
		"internal/server/server.go": "package server\n",
		"internal/installer/installer.go": "package installer\n\nimport _ \"github.com/calionauta/gogogo/internal/" +
			"server\"\n",
	})

	problems, err := check(root)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "does not link the server") {
		t.Fatalf("problems = %v, want the installer→server refusal", problems)
	}
}

// TestCleanTreePasses pins the other direction, so a gate that always fails
// cannot hide behind these tests.
func TestCleanTreePasses(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"cmd/web/main.go": "package main\n\nimport _ \"github.com/calionauta/gogogo/internal/" +
			"server\"\n\nfunc main() {}\n",
		"internal/server/server.go": "package server\n",
		"internal/installer/i.go":   "package installer\n\nimport _ \"github.com/calionauta/gogogo/internal/recipe\"\n",
	})

	problems, err := check(root)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("problems = %v, want none", problems)
	}
}
