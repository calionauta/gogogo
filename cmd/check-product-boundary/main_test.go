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

// TestInfrastructureMustNotNameARemovableSlice pins invariant 3: a package
// under internal/ that a shell boots must not import a slice the operator is
// allowed to drop. `internal/desktopboot` did, and the effect was that the
// reference slice could never leave a product.
func TestInfrastructureMustNotNameARemovableSlice(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"internal/desktopboot/boot.go": "package desktopboot\n\nimport (\n\t_ \"github.com/calionauta/" +
			"gogogo/features/todo\"\n)\n",
	})

	problems, err := check(root)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "removable slice") {
		t.Fatalf("problems = %v, want the infrastructure import", problems)
	}
}

// TestShellMayImportAFeature is the other half: cmd/gui renders todos, so it
// names the slice. Forbidding that would forbid the example.
func TestShellMayImportAFeature(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"cmd/gui/main.go": "package main\n\nimport (\n\t_ \"github.com/calionauta/gogogo/features/" +
			"todo\"\n)\n\nfunc main() {}\n",
	})
	problems, err := check(root)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("a shell was refused: %v", problems)
	}
}

// TestCompositionRootMayNameEveryUnit: internal/server builds the mux, so it
// names units by design — the exemption the rule would be useless without.
func TestCompositionRootMayNameEveryUnit(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"internal/server/chi.go": "package server\n\nimport (\n\t_ \"github.com/calionauta/gogogo/" +
			"features/todo\"\n)\n",
	})
	problems, err := check(root)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("the composition root was refused: %v", problems)
	}
}

// TestCoreClassifiedDirectoryIsNotARemovableSlice: `features/authviews` is half
// of the core `auth` capability, so the shell may name it. The gate reads that
// class from the registry rather than guessing from the path.
func TestCoreClassifiedDirectoryIsNotARemovableSlice(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"internal/ui/shell.go": "package ui\n\nimport (\n\t_ \"github.com/calionauta/gogogo/features/" +
			"authviews\"\n)\n",
	})
	problems, err := check(root)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("a core-classified directory was refused: %v", problems)
	}
}
