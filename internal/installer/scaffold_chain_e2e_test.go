// A scaffold of a scaffold: the second hop a fork actually makes.
//
// Every other installer test starts from the template, so the tree it produces
// is never asked to be a template itself. That is the hop that broke twice:
// the rename trusted constants compiled into the binary (so a fork renamed
// nothing and cloned the template's org), and AGENTS.md rendered its command
// examples from the embedded prompt, so a fork's product advertised the fork's
// binary name instead of its own.
//
// The test drives the real binaries — `go run ./cmd/<name>` in each tree — with
// the target directory pre-created, the way an operator scaffolding a checkout
// they already have. It is slow (two build proofs) and gated on
// RUN_SCAFFOLD_CHAIN=1; `make test-scaffold-chain` runs it, and `make ci-local`
// calls that.
package installer

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// chainSkipDirs are the trees that carry no source identity and would triple
// the copy: VCS metadata, dependencies, runtime data, scratch.
var chainSkipDirs = map[string]bool{
	".git": true, "node_modules": true, "data": true, "tmp": true,
	".commandcode": true, "coverage.out": true,
}

func TestScaffoldChainSecondHopKeepsTheProductIdentity(t *testing.T) {
	if os.Getenv("RUN_SCAFFOLD_CHAIN") != "1" {
		t.Skip("slow (two scaffold build proofs): set RUN_SCAFFOLD_CHAIN=1, or run `make test-scaffold-chain`")
	}
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	first := filepath.Join(work, "first")
	second := filepath.Join(work, "second")

	// Hop 1: the template scaffolds a product in place.
	if err := copyTree(repo, first); err != nil {
		t.Fatalf("copy the template: %v", err)
	}
	scaffoldIn(t, repo, "./cmd/gogogo", first, "hopb", "e2e")

	// Hop 2: the PRODUCT scaffolds the next product. Its own binary, its own
	// module, no constant from the template in sight.
	if err := copyTree(first, second); err != nil {
		t.Fatalf("copy the first product: %v", err)
	}
	scaffoldIn(t, first, "./cmd/hopb", second, "hopc", "acme")

	assertIdentity(t, second)
}

// scaffoldIn runs one scaffold in place: the target exists, so no clone.
func scaffoldIn(t *testing.T, cwd, bin, dir, name, owner string) {
	t.Helper()
	out, err := runIn(t.Context(), cwd, "go", "run", bin,
		"--name", name, "--owner", owner,
		"--no-tui", "--yes", "--shape", "api", "--recipes", "none",
		"--dir", dir)
	if err != nil {
		t.Fatalf("scaffold %s → %s: %v\n%s", bin, dir, err, out)
	}
}

// assertIdentity is what the second hop must leave behind: the new name in the
// module, in the tool's own constants, and in the instructions the product's
// agents read. The base marker keeps the tree it came from, which is the one
// place the previous name belongs.
func assertIdentity(t *testing.T, root string) {
	t.Helper()
	gomod := readChainFile(t, root, "go.mod")
	if !strings.HasPrefix(gomod, "module github.com/acme/hopc\n") {
		t.Fatalf("go.mod = %q, want module github.com/acme/hopc", gomod)
	}
	run := readChainFile(t, root, filepath.Join("internal", "installer", "run.go"))
	for _, want := range []string{`templateOwner = "acme"`, `templateRepo  = "hopc"`} {
		if !strings.Contains(run, want) {
			t.Errorf("the tool's own identity is stale: %q missing", want)
		}
	}
	agents := readChainFile(t, root, "AGENTS.md")
	if !strings.Contains(agents, "hopc recipe list") {
		t.Error("AGENTS.md advertises the previous binary: the command examples must name THIS tree's tool")
	}
	if strings.Contains(agents, "hopb recipe list") {
		t.Error("AGENTS.md still tells an agent to run the previous product's binary")
	}
	if !strings.Contains(agents, "e2e/hopb") {
		t.Error("AGENTS.md lost its base marker: the provenance line must name the tree it came from")
	}

	// The product must build with the binary a user would run.
	if out, err := runIn(t.Context(), root, "go", "build", "-o", os.DevNull, "./cmd/web"); err != nil {
		t.Fatalf("the second product does not build: %v\n%s", err, out)
	}
}

func readChainFile(t *testing.T, root, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(raw)
}

// copyTree copies src into dst, skipping the directories that carry no source
// identity. It is a test helper, not a general utility: no metadata, no links.
//
// Reads go through os.Root: a walk follows whatever the tree says, and the
// fixture trees are copies of a checkout, so a symlink escaping the copy would
// otherwise reach the real one.
func copyTree(src, dst string) error {
	root, rerr := os.OpenRoot(src)
	if rerr != nil {
		return rerr
	}
	defer root.Close()
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		if info.IsDir() {
			if chainSkipDirs[info.Name()] && rel != "." {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		in, oerr := root.Open(rel)
		if oerr != nil {
			return oerr
		}
		defer in.Close()
		out, cerr := os.Create(filepath.Join(dst, rel))
		if cerr != nil {
			return cerr
		}
		defer out.Close()
		if _, ierr := io.Copy(out, in); ierr != nil {
			return ierr
		}
		return os.Chmod(filepath.Join(dst, rel), info.Mode().Perm())
	})
}
