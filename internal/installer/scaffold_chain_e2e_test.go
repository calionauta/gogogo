// A product does not scaffold another product, and that is the whole point.
//
// This test used to be called "a scaffold of a scaffold": it drove
// `go run ./cmd/hopb` in a generated product and asserted the second hop kept
// the product's identity. That was correct while `cmd/<product>` WAS the
// evolution CLI. It is not any more: the product ships the CONSUMER pair
// (`cmd/<product>` + `cmd/<product>-mcp`), neither of which can scaffold —
// building a product is the builder's job, and the builder is removed from
// every product.
//
// So the assertion inverted, and it is the more valuable one: the hop that used
// to be exercised is now a hop that must FAIL, loudly, instead of half-working
// because a consumer binary happened to accept `--name`.
//
// What the first hop leaves behind still matters, and it is checked here: the
// product carries its own identity and its own commands, with no name from the
// template in sight.
//
// The test drives the real binaries (`go run ./cmd/<name>`) and is slow (a
// build proof); it is gated on RUN_SCAFFOLD_CHAIN=1, `make test-scaffold-chain`
// runs it, and `make ci-local` calls that.
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

func TestProductCannotScaffoldAndKeepsItsIdentity(t *testing.T) {
	if os.Getenv("RUN_SCAFFOLD_CHAIN") != "1" {
		t.Skip("slow (a scaffold build proof): set RUN_SCAFFOLD_CHAIN=1, or run `make test-scaffold-chain`")
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

	// The product's identity is its own, and the builder is gone.
	assertIdentity(t, first)

	// Hop 2 must not exist: a product cannot scaffold. The consumer CLI is
	// `cmd/hopb`, and it takes no `--name` — it drives the running server.
	if err := copyTree(first, second); err != nil {
		t.Fatalf("copy the first product: %v", err)
	}
	out, hopErr := runIn(t.Context(), first, "go", "run", "./cmd/hopb",
		"--name", "hopc", "--owner", "acme",
		"--no-tui", "--yes", "--shape", "api", "--recipes", "none",
		"--dir", second)
	if hopErr == nil {
		t.Fatalf("a product scaffolded another product; the builder must not ship:\n%s", out)
	}
	if !strings.Contains(string(out), "unknown") && !strings.Contains(string(out), "usage") {
		t.Errorf("the refusal must name the problem, got:\n%s", out)
	}
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
	if !strings.HasPrefix(gomod, "module github.com/e2e/hopb\n") {
		t.Fatalf("go.mod = %q, want module github.com/e2e/hopb", gomod)
	}
	run := readChainFile(t, root, filepath.Join("internal", "installer", "run.go"))
	for _, want := range []string{`templateOwner = "e2e"`, `templateRepo  = "hopb"`} {
		if !strings.Contains(run, want) {
			t.Errorf("the tool's own identity is stale: %q missing", want)
		}
	}
	agents := readChainFile(t, root, "AGENTS.md")
	// The evolution verbs belong to `gogogo`, not to the product's binary: a
	// product ships the consumer pair, which has no `recipe` and no `--check`.
	// Naming `hopb` here sent an agent to run a verb the product does not have.
	if !strings.Contains(agents, "gogogo recipe list") {
		t.Error("AGENTS.md must name the evolution CLI for the recipe verbs")
	}
	if strings.Contains(agents, "hopb recipe list") || strings.Contains(agents, "hopb migrate") {
		t.Error("AGENTS.md advertises evolution verbs on the product's own binary")
	}
	// The base marker names the TEMPLATE the product came from, never the
	// product itself: that is what makes `--check-update` able to say whether
	// this tree is behind upstream.
	if !strings.Contains(agents, "scaffolded-from: calionauta/gogogo@") {
		t.Error("AGENTS.md lost its base marker: the provenance line must name the template it came from")
	}

	// The product must build with the binaries a user would run: the server and
	// both halves of the consumer pair.
	if out, err := runIn(t.Context(), root, "go", "build", "-o", os.DevNull,
		"./cmd/web", "./cmd/hopb", "./cmd/hopb-mcp"); err != nil {
		t.Fatalf("the product does not build: %v\n%s", err, out)
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
