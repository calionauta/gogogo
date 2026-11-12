// Installer CLI: `recipe test`, and the one derivation both the CLI and
// `recipe check` read to answer "which tests belong to this recipe".
//
// Why the answer is derived and not declared: the receipt already enumerates
// every installed file with its hash, test files included, so a second list in
// the declaration would be a second source of truth that can only disagree with
// the first. A recipe's packages are the directories its own files live in —
// that is exactly the scope `go test` needs, and it moves with the code when
// the author adds a package.
package installer

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/calionauta/gogogo/internal/recipe"
)

// recipeTestArgs is the `recipe test` surface: one recipe (or every installed
// one), the checkout, and the two knobs a caller actually wants — race off for
// a TDD loop, and a single test name while iterating.
type recipeTestArgs struct {
	id   string
	dir  string
	race bool
	list bool
	run  string
}

func parseRecipeTestArgs(args []string) (recipeTestArgs, error) {
	opt := recipeTestArgs{race: true}
	fs := flag.NewFlagSet("recipe test", flag.ContinueOnError)
	fs.StringVar(&opt.dir, "dir", ".", "checkout to test")
	fs.BoolVar(&opt.race, "race", true, "run with the race detector (the gate's mode)")
	fs.BoolVar(&opt.list, "list", false, "print the derived packages instead of running them")
	fs.StringVar(&opt.run, "run", "", "pass -run <regexp> to the test run")
	fs.SetOutput(io.Discard)

	rest := args
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		opt.id = rest[0]
		rest = rest[1:]
	}
	if err := fs.Parse(rest); err != nil {
		return opt, &ExitError{code: 1, msg: "recipe test: " + err.Error()}
	}
	if opt.id == "" && fs.NArg() > 0 {
		opt.id = fs.Arg(0)
	}
	if fs.NArg() > 1 {
		return opt, &ExitError{code: 1, msg: "recipe test takes at most one recipe id"}
	}
	return opt, nil
}

// recipeTestPackages derives the Go packages a recipe owns: every directory its
// receipt lists a .go file in, plus the Register package (older receipts predate
// the Files map), kept only when the directory still exists and holds Go source.
//
// A directory the user deleted is skipped on purpose: `go test` on a missing
// path fails the run, and a deleted file is a decision the drift report already
// answers, not a test failure.
func recipeTestPackages(root string, r recipe.Receipt) []string {
	seen := map[string]bool{}
	add := func(rel string) {
		rel = filepath.ToSlash(strings.TrimPrefix(rel, "./"))
		if rel == "" || rel == "." || seen[rel] {
			return
		}
		dir := filepath.Join(root, filepath.FromSlash(rel))
		if !hasGoFiles(dir) {
			return
		}
		seen[rel] = true
	}
	for path := range r.Files {
		if strings.HasSuffix(path, ".go") {
			add(filepath.Dir(path))
		}
	}
	if r.Register != "" {
		add(r.Register)
	}
	out := make([]string, 0, len(seen))
	for rel := range seen {
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

// recipeTestFiles is the inventory of test files that are ACTUALLY ON DISK,
// derived rather than declared: the directories the receipt points at are where
// the recipe's tests live, so one read of the tree answers the question no
// second list could.
//
// Two directions matter, and they are not symmetrical:
//   - a receipt entry whose file is gone does not count (the user deleted it,
//     and `recipe status` already reports that as missing);
//   - a test the author added next to installed code DOES count (it is real
//     coverage, even though an update would not carry it).
func recipeTestFiles(root string, r recipe.Receipt) []string {
	seen := map[string]bool{}
	for _, pkg := range recipeTestPackages(root, r) {
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(pkg)))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), "_test.go") {
				seen[pkg+"/"+e.Name()] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for path := range seen {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

// recipeTestScope is the run scope: the recipe's packages that actually carry a
// test file. Compiling a package with no tests proves nothing `go build` does not
// already prove, and the point of the command is the tests.
func recipeTestScope(root string, r recipe.Receipt) []string {
	withTests := map[string]bool{}
	for _, file := range recipeTestFiles(root, r) {
		withTests[filepath.ToSlash(filepath.Dir(file))] = true
	}
	out := make([]string, 0, len(withTests))
	for _, pkg := range recipeTestPackages(root, r) {
		if withTests[pkg] {
			out = append(out, pkg)
		}
	}
	return out
}

// hasGoFiles reports whether dir exists and holds at least one .go file. Both
// halves matter: a dir of .templ components with no tests is still a package
// `go test` will accept, but a dir that does not exist is not a scope at all.
func hasGoFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			return true
		}
	}
	return false
}

// recipeTestPlan is the derived scope of one run: the packages to test, and the
// recipes that had nothing to test (reported, not silently skipped).
type recipeTestPlan struct {
	pkgs     []string
	testless []string
}

// planRecipeTests resolves the scope for every requested recipe. It is split
// from runRecipeTest so the CLI's control flow stays readable: resolve, report,
// then either print or execute.
func planRecipeTests(root string, ids []string, stdout io.Writer) (recipeTestPlan, error) {
	var plan recipeTestPlan
	for _, id := range ids {
		receipt, lerr := recipe.LoadReceipt(root, id)
		if lerr != nil {
			return plan, &ExitError{code: 1, msg: "recipe test: " + lerr.Error()}
		}
		scope := recipeTestScope(root, receipt)
		fmt.Fprintf(stdout, "recipe %s: %d test file(s) in %d package(s)\n",
			id, len(recipeTestFiles(root, receipt)), len(scope))
		if len(scope) == 0 {
			fmt.Fprintf(stdout, "  no package carries a _test.go — nothing to run\n")
			plan.testless = append(plan.testless, id)
			continue
		}
		for _, p := range scope {
			fmt.Fprintf(stdout, "  ./%s\n", p)
		}
		plan.pkgs = append(plan.pkgs, scope...)
	}
	return plan, nil
}

// runRecipeTest runs the derived scope through the module's own toolchain, from
// the checkout root so the module path resolves. It execs `go test` rather than
// calling testing.Main: the gate must run the SAME binary CI runs, with the same
// build tags and the same race detector, or it proves something else.
func runRecipeTest(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	opt, err := parseRecipeTestArgs(args)
	if err != nil {
		return err
	}
	root, aerr := filepath.Abs(opt.dir)
	if aerr != nil {
		return &ExitError{code: 1, msg: "recipe test: " + aerr.Error()}
	}
	if !looksLikeTemplate(root) {
		return &ExitError{code: 1, msg: "recipe test: " + opt.dir + " is not a gogogo checkout"}
	}
	ids, ierr := statusIDs("test", root, opt.id)
	if ierr != nil {
		return ierr
	}
	if len(ids) == 0 {
		fmt.Fprintln(stdout, "recipe test: no receipts in "+recipe.ReceiptDir+"/ — nothing to test")
		return nil
	}

	plan, perr := planRecipeTests(root, ids, stdout)
	if perr != nil {
		return perr
	}
	if opt.list {
		return nil
	}
	if len(plan.pkgs) == 0 {
		if len(plan.testless) > 0 {
			return &ExitError{code: 1, msg: "recipe test: no test files for " +
				strings.Join(plan.testless, ", ") + " — `recipe check` blocks a route-owning recipe without tests"}
		}
		return nil
	}

	goArgs := []string{"test", "-count=1"}
	if opt.race {
		goArgs = append(goArgs, "-race")
	}
	if opt.run != "" {
		goArgs = append(goArgs, "-run", opt.run)
	}
	for _, p := range plan.pkgs {
		goArgs = append(goArgs, "./"+p)
	}
	fmt.Fprintf(stdout, "\n→ go %s\n\n", strings.Join(goArgs, " "))
	// The caller's context, so a cancelled CLI stops the run instead of
	// leaving a `go test` behind: the process group dies with the terminal,
	// as any `go test` does.
	cmd := exec.CommandContext(ctx, "go", goArgs...)
	cmd.Dir = root
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if cerr := cmd.Run(); cerr != nil {
		return &ExitError{code: 1, msg: "recipe test: " + cerr.Error()}
	}
	return nil
}
