// The measurement: load the module once, classify every exported top-level
// name by who names it, and aggregate per package.
package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"golang.org/x/tools/go/packages"
)

type class int

const (
	// classConsumed is the ordinary case: another package, in production code,
	// names this export. scan records it so a measurement is complete; judge
	// drops it, because there is nothing to report.
	//
	// It is NOT the zero value on purpose. A struct field left unset then reads
	// as classConsumed and disappears from the report, which is a visible
	// mistake; the zero value being classDead would make an unset field look
	// like a finding, and an unset-class bug would ship false positives.
	classConsumed class = iota
	classDead
	classInternal
	classTestOnly
)

// finding is one exported symbol and who names it.
type finding struct {
	name     string // <package path>.<Name>
	pkg      string
	kind     string
	pos      token.Position
	class    class
	extUses  int
	inUses   int
	testUses int
}

// scan loads the module once and classifies every exported top-level name,
// consumed ones included: a measurement that drops half its rows cannot answer
// "how deep is this package", which is the same data read the other way round.
// judge decides what actually gets reported.
//
// Exports are judged only where an export can BE consumed. Two exceptions,
// both structural rather than a name list:
//
//   - `package main` cannot be imported. Its exported names are reachable only
//     through the linker (`-ldflags -X main.BuildTime`), so calling them dead
//     would be a lie that no code change could fix.
//   - `_test.go` files are not a consumer of production API. A symbol named
//     only by another package's test is classTestOnly: a seam worth a look,
//     but a deliberate one far more often than a mistake.
//
// load type-checks the whole module once. Every report this command produces
// reads from it, so `-methods` and the export surface never load twice.
//
// Tests is deliberately off. With Tests:true the loader type-checks the plain
// and the in-package-test variant of the same package on concurrent goroutines
// that share *types.Named, which trips the race detector inside go/types
// itself. Tests are read by markUnloadedUses instead, from the AST, which is
// also what credits build-tag-excluded files.
//
// One load at a time, per process. go/types fans out across the import graph
// and that fan-out has a data race of its own on the 1.27 toolchain (seen ~1
// run in 6, only under -race, inside Checker.isComplete). Serialising the loads
// is not enough on its own — the fan-out is INSIDE one load — so the load also
// runs on a single thread. Neither costs the gate anything: it loads once.
//
// The returned dir is absolute: markUnloadedUses derives a package path from
// filepath.Rel(dir, file), and a relative dir makes that produce garbage
// (`module//catalog`), which silently matches no declaration and reports a
// clean tree. One normalisation at the boundary keeps every join correct.
// loadMu serialises loads. Two packages.Load calls running at once share
// nothing and must not: go/types type-checks each load on its own goroutines,
// and two of those graphs racing is what the detector reports. Measured: each
// real-tree test alone is 6/6 clean, the same two together fail ~1 run in 6.
// The gate itself loads once, so this costs nothing outside tests.
var loadMu sync.Mutex

func load(dir string, withDeps bool) (string, []*packages.Package, error) {
	loadMu.Lock()
	defer loadMu.Unlock()

	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", nil, err
	}
	mode := packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
		packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
		packages.NeedImports
	if withDeps {
		// Only -methods needs the DEPENDENCIES' type packages: it asks whether
		// an interface from an imported package is satisfied. Type-checking every
		// dependency takes a plain load from 1.03s to 3.55s on this tree, which
		// the default gate must never pay.
		mode |= packages.NeedDeps
	}
	cfg := &packages.Config{Dir: dir, Mode: mode}
	pkgs, lerr := packages.Load(cfg, "./...")
	if lerr != nil {
		return "", nil, lerr
	}
	// Judge ONLY this module's packages. With NeedDeps the load also type-checks
	// every dependency, so a rejection inside one of them would turn into "the
	// gate cannot answer" about a tree that is perfectly fine — a gate that
	// cannot answer because of somebody else's code gets switched off. This tree
	// has zero dependency errors today; the rule exists so that stays a
	// non-event instead of an outage. (Observed once while developing this flag:
	// a stale build cache produced `generic method requires go1.27` inside
	// math/rand/v2 and aborted the run; it did not reproduce after a clean
	// rebuild, which is exactly why the dependency must not be able to decide.)
	var ownErrors []string
	for _, p := range pkgs {
		if p.PkgPath != modulePath && !strings.HasPrefix(p.PkgPath, modulePath+"/") {
			continue
		}
		for _, e := range p.Errors {
			ownErrors = append(ownErrors, e.Error())
		}
	}
	if len(ownErrors) > 0 {
		for _, e := range ownErrors {
			fmt.Fprintln(os.Stderr, "check-surface: "+e)
		}
		return "", nil, fmt.Errorf("%d error(s) in this module's packages; the gate cannot answer", len(ownErrors))
	}
	return dir, pkgs, nil
}

func scan(dir string) ([]finding, error) {
	dir, pkgs, err := load(dir, false)
	if err != nil {
		return nil, err
	}
	return scanPackages(dir, pkgs)
}

// scanPackages is scan over an already-loaded module, so a command that wants
// both the export surface and the method report loads the module ONCE.
func scanPackages(dir string, pkgs []*packages.Package) ([]finding, error) {
	decls, loaded := recordDecls(pkgs)
	creditTypedUses(pkgs, decls)

	// Fail open on files this host cannot type-check. `//go:build android`
	// excludes cmd/mobile from the load on a darwin machine, and cmd/mobile is
	// a real consumer of internal/server — so a symbol only android code calls
	// would read as dead here and nowhere else. The same pass reads `_test.go`.
	// It is deliberately one-directional: an AST use can only SUPPRESS a
	// finding, never create one, so an approximation is safe where an exact
	// answer is unavailable.
	if err := markUnloadedUses(dir, decls, loaded); err != nil {
		return nil, err
	}

	out := make([]finding, 0, len(decls))
	for _, f := range decls {
		f.class = classify(*f)
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// classify turns the three use counters into the one class a report needs.
func classify(f finding) class {
	switch {
	case f.extUses > 0:
		return classConsumed
	case f.testUses > 0:
		return classTestOnly
	case f.inUses > 0:
		return classInternal
	default:
		return classDead
	}
}

// recordDecls collects every exported top-level name the type checker saw, plus
// the set of files it did see (so the AST fallback knows what it is missing).
//
// Keyed by string, not by *types.Object: the same name can arrive from more
// than one loaded package (a package and its test variant), and object identity
// does not survive that. The name is the identity Go guarantees.
func recordDecls(pkgs []*packages.Package) (map[string]*finding, map[string]bool) {
	decls := map[string]*finding{}
	generated := map[string]bool{}
	loaded := map[string]bool{}
	for _, p := range pkgs {
		for _, f := range append(append([]string{}, p.GoFiles...), p.CompiledGoFiles...) {
			loaded[f] = true
		}
		if p.TypesInfo == nil || p.Name == "main" || strings.HasSuffix(p.PkgPath, ".test") {
			continue
		}
		for _, f := range p.Syntax {
			if ast.IsGenerated(f) {
				generated[p.Fset.Position(f.Pos()).Filename] = true
			}
		}
		for ident, obj := range p.TypesInfo.Defs {
			if obj == nil || !obj.Exported() {
				continue
			}
			kind, ok := exportedKind(obj)
			if !ok {
				continue
			}
			pos := p.Fset.Position(ident.Pos())
			if generated[pos.Filename] || strings.HasSuffix(pos.Filename, "_test.go") {
				continue
			}
			name := obj.Pkg().Path() + "." + obj.Name()
			if _, dup := decls[name]; dup {
				continue
			}
			decls[name] = &finding{name: name, pkg: p.PkgPath, kind: kind, pos: pos}
		}
	}
	return decls, loaded
}

// creditTypedUses adds the type-checked uses: same-package ones as inUses,
// another package's as extUses, and another package's TEST as testUses.
func creditTypedUses(pkgs []*packages.Package, decls map[string]*finding) {
	for _, p := range pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for ident, obj := range p.TypesInfo.Uses {
			if obj == nil || obj.Pkg() == nil || !obj.Exported() {
				continue
			}
			if _, ok := exportedKind(obj); !ok {
				continue
			}
			f, ok := decls[obj.Pkg().Path()+"."+obj.Name()]
			if !ok {
				continue
			}
			if obj.Pkg().Path() == p.PkgPath {
				f.inUses++
				continue
			}
			// The USE's own file decides whether this is a test consumer, not
			// the package: a package with any test file would otherwise have
			// every production use demoted to a test use. `cmd/desktop` is a
			// consumer like any other here — a `main` package cannot declare an
			// export worth judging, but it certainly calls one.
			if strings.HasSuffix(p.Fset.Position(ident.Pos()).Filename, "_test.go") {
				f.testUses++
				continue
			}
			f.extUses++
		}
	}
}

// judge keeps the findings a report is about: everything except a name another
// package already consumes in production code.
func judge(findings []finding) []finding {
	out := make([]finding, 0, len(findings))
	for _, f := range findings {
		if f.class != classConsumed {
			out = append(out, f)
		}
	}
	return out
}

// surfaceRow is one package's export depth: how many exported names it offers,
// and how many of those some other package uses in production code.
type surfaceRow struct {
	pkg    string
	export int
	used   int
	// Why the rest are not used, so a low ratio can be read instead of just
	// distrusted: inUses only from its own package, testUses only from a test,
	// dead from nobody.
	own, test, dead int
}

// surfaceRows aggregates the measurement per package, worst ratio first.
// Packages below minExport are skipped: three exports, one used, is not a
// finding, it is a small package.
func surfaceRows(findings []finding, minExport int) []surfaceRow {
	byPkg := map[string]*surfaceRow{}
	for _, f := range findings {
		r := byPkg[f.pkg]
		if r == nil {
			r = &surfaceRow{pkg: f.pkg}
			byPkg[f.pkg] = r
		}
		r.export++
		switch f.class {
		case classConsumed:
			r.used++
		case classTestOnly:
			r.test++
		case classInternal:
			r.own++
		default:
			r.dead++
		}
	}
	out := make([]surfaceRow, 0, len(byPkg))
	for _, r := range byPkg {
		if r.export >= minExport {
			out = append(out, *r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ri := float64(out[i].used) / float64(out[i].export)
		rj := float64(out[j].used) / float64(out[j].export)
		if ri != rj {
			return ri < rj
		}
		return out[i].pkg < out[j].pkg
	})
	return out
}

func deadNames(findings []finding) []string {
	var out []string
	for _, f := range findings {
		if f.class == classDead {
			out = append(out, f.name)
		}
	}
	sort.Strings(out)
	return out
}

// subtracting returns the names in `names` that are not in `allow`.
func subtracting(names, allow []string) []string {
	var out []string
	for _, n := range names {
		if !slices.Contains(allow, n) {
			out = append(out, n)
		}
	}
	return out
}
