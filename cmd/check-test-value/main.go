// SCOPE:core — Test-value guard: every Test must be able to fail, and pin behavior not just err==nil.
//
// A test that cannot fail reads as coverage and survives review while hiding
// the regression it was written for (lived bug: TestTodoRecordsNotBroadcastViaHub
// stayed green at two windows because its predicate matched a string the wire
// never carries — see skills/gogogo-coding-standards/references/go-testing.md).
//
// Two classes, deliberately split by exit code:
//
//	vacuous  — the Test body holds no failure call at all (no t.Error/Fatal,
//	           no assert/require, no asserting helper, no goleak check). It
//	           fails only on panic. Exits 1. This is the blocking half.
//	err-only — every failure call only guards plumbing (`if err != nil` →
//	           t.Fatal, assert.NoError). It pins that the call succeeded, not
//	           what it did. Printed as a warning, exits 0 unless -fail-err-only.
//	           A negative contract (`if err == nil` → t.Fatal, "want error")
//	           counts as specific, not err-only: it pins the failure mode.
//
// Assertion sinks are resolved, not guessed: a first pass over the package's
// own test files records every helper whose body can fail (t.Error/Fatal,
// testify, goleak, or another recorded helper, to a fixpoint), and calls to
// those helpers count as pins. A helper that only sets up fixtures is the
// known hole — a setup-only test is obvious in review, a wrong predicate is
// not, and this gate aims at the second.
//
// Excluded: TestMain, Examples (the `// Output:` comment is the assertion),
// Benchmarks/Fuzz functions, and tests whose testing param is `_` (an
// explicit signal that the assertion lives outside the body: -race, as in
// TestDocStoreConcurrentAccessIsSafe, or a leak checker).
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var failErrOnly = flag.Bool("fail-err-only", false,
	"exit 1 also when a test only asserts err==nil (default: warn only)")

var quiet = flag.Bool("quiet", false,
	"print err-only warnings only in loud mode (vacuous still fails)")

// testingPkg is the import name failure calls are resolved against.
const testingPkg = "testing"

// vacuousMsg / errOnlyMsg are the two verdicts. Long lines live here as
// constants (lll), not at the Printf call sites.
const vacuousMsg = "%s:%d: Test%s fails only on panic — no assertion pins its behavior. " +
	"Assert the wire symptom, not just err==nil; see skills/gogogo-coding-standards/references/go-testing.md\n"

const errOnlyMsg = "%s:%d: ⚠ Test%s only asserts err==nil — it pins plumbing, not behavior. " +
	"Assert the wire symptom (status code, body, patch); see references/go-testing.md\n"

// failureMethods are testing.T/B methods that record a failure. Skip* is
// deliberately absent: skipping is not asserting.
var failureMethods = map[string]bool{
	"Fail": true, "FailNow": true, "Fatal": true,
	"Fatalf": true, "Error": true, "Errorf": true,
}

// testifyPkgs are assertion packages recognized by import name. Method-level
// classification happens in the call site.
var testifyPkgs = map[string]bool{
	"assert": true, "require": true,
}

// testifyErrOnly are testify assertions that only pin plumbing: the call
// succeeded (or failed), not what it did.
var testifyErrOnly = map[string]bool{
	"NoError": true, "Error": true, "Nil": true, "NotNil": true,
	"NoPanic": true, "Panics": true, "NotPanics": true,
}

type finding struct {
	pos   token.Position
	test  string
	class string // "vacuous" | "err-only"
}

func main() {
	flag.Parse()
	roots := flag.Args()
	if len(roots) == 0 {
		roots = []string{"."}
	}
	var findings []finding
	for _, root := range roots {
		out, err := scanRoot(root)
		if err != nil {
			fmt.Fprintln(os.Stderr, "check-test-value:", err)
			os.Exit(2)
		}
		findings = append(findings, out...)
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].pos.Filename != findings[j].pos.Filename {
			return findings[i].pos.Filename < findings[j].pos.Filename
		}
		return findings[i].pos.Line < findings[j].pos.Line
	})
	failed := false
	for _, f := range findings {
		if f.class == "vacuous" {
			fmt.Printf(vacuousMsg, f.pos.Filename, f.pos.Line, f.test)
			failed = true
		} else if !*quiet {
			fmt.Printf(errOnlyMsg, f.pos.Filename, f.pos.Line, f.test)
			if *failErrOnly {
				failed = true
			}
		} else if *failErrOnly {
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
	fmt.Println("check-test-value: no vacuous tests")
}

// pkgFiles holds one directory's parsed test files.
type pkgFiles struct {
	fset  *token.FileSet
	files []*ast.File
}

func scanRoot(root string) ([]finding, error) {
	byDir, err := collectTestFiles(root)
	if err != nil {
		return nil, err
	}
	var out []finding
	for _, pf := range byDir {
		out = append(out, classifyPackage(pf)...)
	}
	return out, nil
}

// collectTestFiles walks root and parses every *_test.go once, grouped by
// directory (helpers are package-scoped, so classification needs the whole
// directory, not one file at a time).
func collectTestFiles(root string) (map[string]*pkgFiles, error) {
	byDir := map[string]*pkgFiles{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// A path the walk itself cannot read: loud, not silent — a
			// gate that skips what it cannot read fails open.
			fmt.Fprintln(os.Stderr, "check-test-value: walk", path+":", err)
			return nil
		}
		if d.IsDir() {
			if skipDir(path, d.Name(), root) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isTestFile(path) {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr == nil {
			// Unparseable test files are skipped here: the build/vet gate
			// owns that failure, not this one. Inverted (== nil) rather
			// than an early return so the skip stays a skip, not a finding.
			dir := filepath.Dir(path)
			pf, ok := byDir[dir]
			if !ok {
				pf = &pkgFiles{fset: fset}
				byDir[dir] = pf
			}
			pf.files = append(pf.files, f)
		}
		return nil
	})
	return byDir, err
}

// skipDir reports version-control, dependency, and hidden trees — but never
// the walk root itself: invoked as `.`, its name starts with a dot and would
// otherwise prune the whole tree (a gate that matches nothing is broken).
func skipDir(path, name, root string) bool {
	if path == root {
		return false
	}
	return name == ".git" || name == "vendor" || name == "node_modules" ||
		strings.HasPrefix(name, ".")
}

func isTestFile(path string) bool {
	return strings.HasSuffix(path, "_test.go") && !strings.HasSuffix(path, "_templ.go")
}

// classifyPackage runs the two passes over one directory: record every
// helper that can fail, then judge every Test against that set.
func classifyPackage(pf *pkgFiles) []finding {
	helpers := assertingHelpers(pf)
	var out []finding
	for _, f := range pf.files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			name := fn.Name.Name
			if !strings.HasPrefix(name, "Test") || name == "TestMain" {
				continue
			}
			if isBenchmarkOrFuzz(fn) || blankTestingParam(fn) {
				continue
			}
			if v := classifyTest(fn, helpers); v != nil {
				v.pos = pf.fset.Position(fn.Pos())
				v.test = strings.TrimPrefix(name, "Test")
				out = append(out, *v)
			}
		}
	}
	return out
}

// blankTestingParam reports `func TestXxx(_ *testing.T)`: the author
// explicitly delegates the assertion outside the body (-race, goleak via
// TestMain). Greppable, reviewable, exempt.
