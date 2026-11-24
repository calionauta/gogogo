// Exported methods that nothing calls and no interface requires.
//
// A method's consumer is usually an interface, which is why check-surface does
// not judge methods by the "does anyone name it" rule. But "called through an
// interface" and "called by nobody" are different facts, and only the first is
// legitimate. This file separates them.
//
// It is the one gap no other gate covers, verified on this tree with a real
// uncalled method on internal/queue.RetryConfig:
//   - `go tool deadcode` reports only free functions reachable from a main; a
//     method is not one, so it says nothing;
//   - `golangci-lint`'s `unused` skips exported identifiers deliberately,
//     because for a library it cannot prove an outside importer is absent;
//   - `iface` judges INTERFACE method sets (pollution), not concrete types.
package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// methodGap is an exported method declared on an exported type, in this module,
// that is neither selected by a call nor required by an interface the receiver
// satisfies.
type methodGap struct {
	name string // <package path>.<Type>.<Method>
	pkg  string
	pos  token.Position
}

// methodCandidate is declared metadata for one exported method.
type methodCandidate struct {
	key  string
	pkg  string
	recv *types.Named
	name string
	pos  token.Position
}

// collectMethodCandidates records every exported method declared on an exported
// named type, skipping generated files (a generated setter is not hand-written
// surface, and it is regenerated rather than deleted).
func collectMethodCandidates(pkgs []*packages.Package) []methodCandidate {
	var out []methodCandidate
	for _, p := range pkgs {
		if p.TypesInfo == nil || p.Name == mainPackage || strings.HasSuffix(p.PkgPath, ".test") {
			continue
		}
		for ident, obj := range p.TypesInfo.Defs {
			fn, ok := obj.(*types.Func)
			if !ok || !fn.Exported() {
				continue
			}
			sig, ok := fn.Type().(*types.Signature)
			if !ok || sig.Recv() == nil {
				continue
			}
			named := receiverNamed(sig.Recv().Type())
			if named == nil || !named.Obj().Exported() {
				continue
			}
			if inGeneratedFile(p, ident.Pos()) {
				continue
			}
			out = append(out, methodCandidate{
				key:  p.PkgPath + "." + named.Obj().Name() + "." + fn.Name(),
				pkg:  p.PkgPath,
				recv: named,
				name: fn.Name(),
				pos:  p.Fset.Position(ident.Pos()),
			})
		}
	}
	return out
}

// selectedMethods is the set of <pkg>.<Type>.<Method> keys the type checker saw
// used as a selection (`x.M`, `x.M()`), which is the direct call shape.
func selectedMethods(pkgs []*packages.Package) map[string]bool {
	called := map[string]bool{}
	for _, p := range pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, sel := range p.TypesInfo.Selections {
			if sel.Kind() != types.MethodVal && sel.Kind() != types.MethodExpr {
				continue
			}
			fn, ok := sel.Obj().(*types.Func)
			if !ok {
				continue
			}
			if key := methodKey(fn); key != "" {
				called[key] = true
			}
		}
	}
	return called
}

// methodKey is <pkg>.<Type>.<Method> for a method function, or "" when the
// receiver cannot name a declared type.
func methodKey(fn *types.Func) string {
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return ""
	}
	named := receiverNamed(sig.Recv().Type())
	if named == nil {
		return ""
	}
	pkg := named.Obj().Pkg()
	if pkg == nil {
		return "" // a synthetic type (instantiated generic, error type): not ours
	}
	return pkg.Path() + "." + named.Obj().Name() + "." + fn.Name()
}

// satisfiedByAny reports whether the receiver implements any interface that the
// module declares or imports.
//
// This is what keeps an interface implementation out of the report: a type
// satisfying `io.Closer` is called THROUGH the interface, so `x.Close()` never
// appears at the call site and the method would otherwise read as dead. The
// check is real satisfaction (types.Implements over the receiver's method set),
// not a name match — a name match would credit every `Close` in the module the
// moment one interface declared one.
func satisfiedByAny(recv *types.Named, ifaces []*types.Interface) bool {
	// Both value and pointer receivers count: an interface is satisfied by
	// either form, and which one a type declares is a detail of where the
	// method is written, not of whether it is reachable.
	ptr := types.NewPointer(recv)
	for _, iface := range ifaces {
		if types.Implements(recv, iface) || types.Implements(ptr, iface) {
			return true
		}
	}
	return false
}

// moduleInterfaces collects every interface type the module can see: declared
// in module packages, plus the interface types of imported packages that module
// types are assigned to or returned as.
// importedTypesPackages returns the *types.Package of every import of p,
// including transitive ones, since an interface can come from a dependency of a
// dependency.
func importedTypesPackages(p *packages.Package) []*types.Package {
	var out []*types.Package
	seen := map[*types.Package]bool{}
	var walk func(*types.Package)
	walk = func(tp *types.Package) {
		if tp == nil || seen[tp] {
			return
		}
		seen[tp] = true
		out = append(out, tp)
		for _, imp := range tp.Imports() {
			walk(imp)
		}
	}
	for _, imp := range p.Imports {
		if imp.Types != nil {
			walk(imp.Types)
		}
	}
	return out
}

// addInterfacesOf feeds every interface type name a package exports to add.
func addInterfacesOf(tp *types.Package, add func(types.Type)) {
	scope := tp.Scope()
	if scope == nil {
		return
	}
	for _, name := range scope.Names() {
		tn, ok := scope.Lookup(name).(*types.TypeName)
		if !ok {
			continue
		}
		add(tn.Type())
	}
}

func moduleInterfaces(pkgs []*packages.Package) []*types.Interface {
	var out []*types.Interface
	add := interfaceSink(&out, map[string]bool{})
	for _, p := range pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, obj := range p.TypesInfo.Defs {
			if tn, ok := obj.(*types.TypeName); ok {
				add(tn.Type())
			}
		}
		// Imported packages: `p.Imports` is populated from NeedImports/NeedTypes,
		// but its Types field is nil unless the dependency was type-checked
		// (NeedDeps). Walk every import of every loaded package instead, which is
		// where an interface like casbin's `persist.Adapter` lives — the one that
		// keeps internal/rbac.DBAdapter's SavePolicy alive.
		for _, imp := range importedTypesPackages(p) {
			addInterfacesOf(imp, add)
		}
	}
	return out
}

// interfaceSink returns the collector that decides which interface types may
// keep a method alive, appending to out.
//
// It is a named function rather than a closure because every rule in it exists
// because ignoring that rule produced a wrong answer, and a rule buried in a
// closure can only be tested through a whole module fixture. The empty-interface
// guard below is unreachable that way (nothing declares `any` as a TypeName), so
// without this seam it would be untested code that reads as protection.
func interfaceSink(out *[]*types.Interface, seen map[string]bool) func(types.Type) {
	return func(t types.Type) {
		if named, ok := t.(*types.Named); ok {
			t = named.Underlying()
		}
		iface, ok := t.(*types.Interface)
		if !ok {
			return
		}
		// An EMPTY interface (`any`, `interface{}`) is satisfied by every type,
		// so crediting it would mark the whole module reachable and the report
		// would always be empty. It is a constraint with no methods, which is
		// not what keeps a method alive.
		if iface.NumMethods() == 0 {
			return
		}
		key := iface.String()
		if seen[key] {
			return
		}
		seen[key] = true
		*out = append(*out, iface)
	}
}

// methodGaps is the report: candidates that are neither selected nor required
// by an interface their receiver satisfies.
func methodGaps(pkgs []*packages.Package) []methodGap {
	ifaces := moduleInterfaces(pkgs)
	cands := collectMethodCandidates(pkgs)
	if len(cands) == 0 {
		return nil
	}
	called := selectedMethods(pkgs)
	for k := range selectedMethodsInTests(pkgs) {
		called[k] = true
	}
	// Memoise satisfaction per receiver type: it is a method-set query, and a
	// type with 10 methods would compute it 10 times.
	satisfied := map[*types.Named]bool{}
	var out []methodGap
	for _, c := range cands {
		if called[c.key] {
			continue
		}
		ok, seen := satisfied[c.recv]
		if !seen {
			ok = satisfiedByAny(c.recv, ifaces)
			satisfied[c.recv] = ok
		}
		if ok {
			continue
		}
		out = append(out, methodGap{name: c.key, pkg: c.pkg, pos: c.pos})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// receiverNamed is T for `*T` and `T`; nil for anything that cannot name a
// declared type (a type parameter, an instantiated generic).
func receiverNamed(t types.Type) *types.Named {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return nil
	}
	return named
}

// inGeneratedFile reports whether pos falls inside a generated file, which is
// detected once per file with ast.IsGenerated rather than by name.
func inGeneratedFile(p *packages.Package, pos token.Pos) bool {
	for _, f := range p.Syntax {
		if f.Pos() <= pos && pos <= f.End() {
			return ast.IsGenerated(f)
		}
	}
	return false
}
