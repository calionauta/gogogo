// The test-file credit pass for the method report.
//
// The type loader runs without Tests (see load): the test variant of a package
// is never type-checked, so a call that only a test makes never appears in
// TypesInfo.Selections. Without this pass every test-only seam reads as dead —
// a.SetOAuthProviders(...) and t.Cleanup(a.DisableOpen) are real uses, and a
// gate that calls them dead is a gate people switch off.
//
// It is a separate file because the rule is a small parser of its own: what a
// selector's receiver is, and when that receiver is knowable from source. It
// reads the AST only; nothing here type-checks.
package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// selectedMethodsInTests credits `x.M()` and `x.M` found in `_test.go` files.
//
// The type loader runs without Tests (see load), so the test variant of a
// package is never type-checked and its selections never appear in
// TypesInfo.Selections. Measured on this tree that gap made every test-only
// seam look dead: a.SetOAuthProviders(...) and t.Cleanup(a.DisableOpen) are
// real uses, and a gate that calls them dead is a gate people switch off.
//
// A test file selects a method on a value whose STATIC type the AST cannot
// know, so this pass credits by method NAME — but only inside the package that
// DECLARES that method, and only for a name that package actually declares. A
// bare name match across the module would credit every `String()` in the tree
// the moment one test called one (measured: A.Shared() in a test hid a dead
// B.Shared() in the same package, which is the bug the receiver check is for).
//
// Narrowing to the declaring package is safe in the fail-open direction: a test
// can only call an unexported-in-practice method from its own package, and a
// cross-package call goes through the exported API, which the selector scan
// already sees. Widening beyond that is what produces false confidence.
func selectedMethodsInTests(pkgs []*packages.Package) map[string]bool {
	out := map[string]bool{}
	// Names declared per package, so a test only credits its own package's
	// methods.
	byPkg := map[string]map[string][]string{}
	for _, c := range collectMethodCandidates(pkgs) {
		names, ok := byPkg[c.pkg]
		if !ok {
			names = map[string][]string{}
			byPkg[c.pkg] = names
		}
		names[c.name] = append(names[c.name], c.key)
	}
	if len(byPkg) == 0 {
		return out
	}
	pkgTypes := pkgTypeNames(pkgs)
	fset := token.NewFileSet()
	for _, p := range pkgs {
		if p.TypesInfo == nil {
			continue
		}
		names, ok := byPkg[p.PkgPath]
		if !ok {
			continue
		}
		dir := packageDir(p)
		if dir == "" {
			continue
		}
		tests, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
		if err != nil {
			continue
		}
		for _, tf := range tests {
			f, perr := parser.ParseFile(fset, tf, nil, parser.SkipObjectResolution)
			if perr != nil || ast.IsGenerated(f) {
				continue
			}
			creditTestFile(f, names, pkgTypes[p.PkgPath], out)
		}
	}
	return out
}

// packageDir is the directory a package's files live in, from whichever list the
// loader populated. A fixture package with only test or generated files has an
// empty GoFiles, which made an earlier version skip its tests entirely.
func packageDir(p *packages.Package) string {
	for _, list := range [][]string{p.GoFiles, p.CompiledGoFiles, p.OtherFiles} {
		if len(list) > 0 {
			return filepath.Dir(list[0])
		}
	}
	return ""
}

// pkgTypeNames is the set of type names a package declares. A test file names
// those types without declaring them itself, so the credit pass needs the
// package's set, not the file's.
func pkgTypeNames(pkgs []*packages.Package) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, p := range pkgs {
		if p.Types == nil {
			continue
		}
		names := map[string]bool{}
		scope := p.Types.Scope()
		for _, n := range scope.Names() {
			if _, ok := scope.Lookup(n).(*types.TypeName); ok {
				names[n] = true
			}
		}
		out[p.PkgPath] = names
	}
	return out
}

// creditTestFile credits the methods one test file can plausibly call.
//
// The rule is a RECEIVER match, not a bare name match. A selector `X.M` is
// credited to `<pkg>.X.M` when the test names `X` as a type in scope, and `M`
// to `<pkg>.T.M` for a receiver variable whose type the file declares. When the
// receiver cannot be resolved the name is still credited ONLY if every
// candidate with that name shares one receiver type — never when two types
// declare the same method name, which is the case that made A.Shared() hide a
// dead B.Shared().
func creditTestFile(f *ast.File, names map[string][]string, pkgTypes map[string]bool, out map[string]bool) {
	// A test names the types its PACKAGE declares (they live in the non-test
	// files) plus any it declares itself; the type-checked scope is the honest
	// source for the first part.
	namesExportedType := func(recvName string) bool {
		return pkgTypes[recvName]
	}
	// Local variables, learned from `x := T{...}` / `x := NewT(...)`: a receiver
	// is usually a VARIABLE (`a.OnChange(...)`), and without its type the
	// selector is unresolvable and the method reads as dead. Measured: this made
	// Presence.OnChange look unreachable while presence_test.go called it.
	varTypes := variableTypes(f, pkgTypes)

	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		keys := names[sel.Sel.Name]
		if len(keys) == 0 {
			return true
		}
		// The receiver's TYPE, when the AST can name one: `A{}.M`, `(*A)(nil).M`,
		// `A.M`, `new(A).M`. Resolving this is the difference between crediting
		// the method the test calls and crediting every same-named sibling.
		if recv := resolveReceiver(sel.X, varTypes, namesExportedType); recv != "" {
			for _, k := range keys {
				if strings.HasSuffix(k, "."+recv+"."+sel.Sel.Name) {
					out[k] = true
				}
			}
			return true
		}
		// A qualified type from an import: `pkg.X.M`. The AST cannot resolve the
		// receiver, so credit only when the name is unambiguous module-wide.
		for _, k := range keys {
			if receiverIsUnique(k, keys) {
				out[k] = true
			}
		}
		return true
	})
}

// receiverTypeName is the type name of a selector's left side when the source
// spells it: a composite literal (`A{}`), a parenthesized or dereferenced
// literal (`(*A)(nil)`), a type conversion or call (`new(A)`), or a bare
// identifier (`A.M`, or a variable the AST cannot type). "" when nothing can be
// read, which the caller treats as ambiguous and credits conservatively.
func receiverTypeName(x ast.Expr) string {
	switch e := x.(type) {
	case *ast.CompositeLit:
		return receiverTypeName(e.Type)
	case *ast.ParenExpr:
		return receiverTypeName(e.X)
	case *ast.StarExpr:
		return receiverTypeName(e.X)
	case *ast.CallExpr:
		// `new(A)` / `A(x)` / `(*A)(nil)`: the callee names the type.
		return receiverTypeName(e.Fun)
	case *ast.Ident:
		return e.Name
	default:
		return ""
	}
}

// receiverIsUnique reports whether every candidate key with the same method
// name belongs to the same receiver TYPE, which makes a bare-name credit safe.
// Two methods called `Shared` on different types is exactly the ambiguity that
// turns this pass into a false pass.
func receiverIsUnique(key string, keys []string) bool {
	recv := key[:strings.LastIndex(key, ".")]
	for _, other := range keys {
		if other[:strings.LastIndex(other, ".")] != recv {
			return false
		}
	}
	return true
}

// variableTypes maps local variable names in a test file to the type they were
// declared with, for the two shapes a test actually writes:
//
//	a := NewPresence(...)   // constructor call whose name starts with the type
//	a := &Presence{...}     // composite literal
//
// Anything else stays unknown, which the caller treats as ambiguous rather than
// guessing: a wrong receiver credit is a false pass.
func variableTypes(f *ast.File, pkgTypes map[string]bool) map[string]string {
	out := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != len(as.Rhs) {
			return true
		}
		for i, lhs := range as.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok {
				continue
			}
			if t := typeFromExpr(as.Rhs[i], pkgTypes); t != "" {
				out[id.Name] = t
			}
		}
		return true
	})
	return out
}

// typeFromExpr is the type an expression constructs, when its name says so:
// `&T{}`, `T{}`, `new(T)`, `NewT(...)` -> "T" if T is a declared type.
func typeFromExpr(e ast.Expr, pkgTypes map[string]bool) string {
	switch v := e.(type) {
	case *ast.UnaryExpr: // &T{...}
		return typeFromExpr(v.X, pkgTypes)
	case *ast.CompositeLit:
		if t := receiverTypeName(v.Type); t != "" && pkgTypes[t] {
			return t
		}
	case *ast.CallExpr: // new(T) / NewT(...) / T(...)
		if t := receiverTypeName(v.Fun); t != "" {
			if pkgTypes[t] {
				return t
			}
			// NewPresence -> Presence
			if t2, ok := strings.CutPrefix(t, "New"); ok && pkgTypes[t2] {
				return t2
			}
		}
	}
	return ""
}

// resolveReceiver is the receiver's type name when the selector names one or a
// variable whose type this file declared.
func resolveReceiver(x ast.Expr, varTypes map[string]string, isType func(string) bool) string {
	if t := receiverTypeName(x); t != "" && isType(t) {
		return t
	}
	if id, ok := x.(*ast.Ident); ok {
		if t, ok := varTypes[id.Name]; ok {
			return t
		}
	}
	return ""
}
