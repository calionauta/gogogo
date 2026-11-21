package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// productMCP is the consumer surface every fixture tree needs, or check()
// reports its absence instead of the thing the test is about.
const productMCP = "cmd/product-mcp/main.go"

// writeTree lays down a minimal checkout: four Go files whose imports the gate
// reads. Only the import lines matter — the gate parses, it does not build.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	if _, ok := files[productMCP]; !ok {
		files[productMCP] = "package main\n\nimport _ \"github.com/calionauta/gogogo/" +
			"internal/productapi\"\n\nfunc main() {}\n"
	}
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

// The invariant the original gate never asked: the product must HAVE a consumer
// surface. A tree with no cmd/product-mcp satisfies "builds without the
// evolution tools" by having no call surface at all — which is exactly the
// state `recipe invoke` living inside internal/installer produced.
func TestProductMustShipAConsumerSurface(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"cmd/web/main.go":                 "package main\n\nfunc main() {}\n",
		"internal/server/server.go":       "package server\n",
		"internal/installer/installer.go": "package installer\n",
	})
	// writeTree seeds the surface; remove it to model the regression.
	if err := os.Remove(filepath.Join(root, productMCP)); err != nil {
		t.Fatal(err)
	}

	problems, err := check(root)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "cmd/product-mcp is missing") {
		t.Fatalf("problems = %v, want the missing consumer surface", problems)
	}
}

// And it must not link the toolchain, or shipping it puts the installer back in
// the product.
func TestConsumerSurfaceMustNotLinkTheEvolutionTools(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"cmd/web/main.go": "package main\n\nfunc main() {}\n",
		productMCP: "package main\n\nimport (\n\t_ \"github.com/calionauta/gogogo/internal/" +
			"installer\"\n)\n\nfunc main() {}\n",
		"internal/server/server.go":       "package server\n",
		"internal/installer/installer.go": "package installer\n",
	})

	problems, err := check(root)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "cmd/product-mcp imports") {
		t.Fatalf("problems = %v, want the product-mcp import", problems)
	}
}

// The client is shared code: it talks HTTP to a running product, so it must not
// link the server, and it must not read the checkout.
func TestConsumerClientStaysInnocuous(t *testing.T) {
	for _, tc := range []struct {
		name, imp, want string
	}{
		{"server", "github.com/calionauta/gogogo/internal/server", "does not link the server"},
		{"installer", "github.com/calionauta/gogogo/internal/installer", "must not read a checkout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeTree(t, root, map[string]string{
				"cmd/web/main.go":           "package main\n\nfunc main() {}\n",
				"internal/server/server.go": "package server\n",
				"internal/productapi/client.go": "package productapi\n\nimport (\n\t_ \"" +
					tc.imp + "\"\n)\n",
			})

			problems, err := check(root)
			if err != nil {
				t.Fatalf("check: %v", err)
			}
			found := false
			for _, p := range problems {
				if strings.Contains(p, "internal/productapi imports") && strings.Contains(p, tc.want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("problems = %v, want one naming %q", problems, tc.want)
			}
		})
	}
}
