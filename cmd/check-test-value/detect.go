// SCOPE:core — test-value detection: helpers that can fail, and the two verdicts.
//
// Second file of cmd/check-test-value, split by seam: main.go owns the CLI
// and the directory walk, this file owns the AST classification. The split
// point is the file-size gate (500 lines), not taste.
package main

import (
	"go/ast"
	"go/token"
	"strings"
)

func blankTestingParam(fn *ast.FuncDecl) bool {
	if fn.Type.Params == nil {
		return false
	}
	for _, p := range fn.Type.Params.List {
		star, ok := p.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		sel, ok := star.X.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != testingPkg || sel.Sel.Name != "T" {
			continue
		}
		for _, n := range p.Names {
			if n.Name == "_" {
				return true
			}
		}
	}
	return false
}

// isBenchmarkOrFuzz reports Benchmark*/Fuzz* (including Test-prefixed
// benchmarks, which take *testing.B).
func isBenchmarkOrFuzz(fn *ast.FuncDecl) bool {
	if strings.HasPrefix(fn.Name.Name, "Benchmark") || strings.HasPrefix(fn.Name.Name, "Fuzz") {
		return true
	}
	for _, p := range fn.Type.Params.List {
		if star, ok := p.Type.(*ast.StarExpr); ok {
			if sel, ok := star.X.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == testingPkg &&
					(sel.Sel.Name == "B" || sel.Sel.Name == "F") {
					return true
				}
			}
		}
	}
	return false
}

// assertingHelpers returns the names of package-level funcs and methods
// (test files only) whose body can fail a test: a t.Error/Fatal call, a
// testify assertion, a goleak check, or a call to another asserting helper
// (fixpoint). Callers of these names pin behavior through them.
func assertingHelpers(pf *pkgFiles) map[string]bool {
	// Every package-level func/method in the test files is a candidate.
	var all []helperDecl
	for _, f := range pf.files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			all = append(all, helperDecl{name: fn.Name.Name, fn: fn})
		}
	}
	// Direct: body holds a failure call on a testing param, a testify
	// assertion, or a goleak check.
	asserting := directlyAsserting(all)
	// Transitive: a helper that only calls asserting helpers asserts too.
	transitivelyAsserting(all, asserting)
	return asserting
}

// helperDecl is one package-level func/method in the test files.
type helperDecl struct {
	name string
	fn   *ast.FuncDecl
}

// directlyAsserting marks helpers whose own body can fail a test: a failure
// call on a testing param, a testify assertion, or a goleak check.
func directlyAsserting(all []helperDecl) map[string]bool {
	asserting := map[string]bool{}
	for _, d := range all {
		ctx := &testCtx{tNames: map[string]bool{}}
		collectTNames(d.fn.Type, ctx)
		w := &failWalk{ctx: ctx}
		ast.Walk(w, d.fn.Body)
		if w.failed {
			asserting[d.name] = true
		}
	}
	return asserting
}

// transitivelyAsserting closes over helpers that only call asserting
// helpers: they assert through the callee.
func transitivelyAsserting(all []helperDecl, asserting map[string]bool) {
	for changed := true; changed; {
		changed = false
		for _, d := range all {
			if asserting[d.name] || !callsAsserting(d.fn.Body, asserting) {
				continue
			}
			asserting[d.name] = true
			changed = true
		}
	}
}

// callsAsserting reports whether body calls any name in the asserting set,
// as a bare function or a method.
func callsAsserting(body ast.Node, asserting map[string]bool) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := c.Fun.(*ast.Ident); ok && asserting[id.Name] {
			found = true
			return false
		}
		if sel, ok := c.Fun.(*ast.SelectorExpr); ok && asserting[sel.Sel.Name] {
			found = true
			return false
		}
		return true
	})
	return found
}

// failWalk marks failed when the subtree can fail a test.
type failWalk struct {
	ctx    *testCtx
	failed bool
}

func (w *failWalk) Visit(n ast.Node) ast.Visitor {
	if w.failed || n == nil {
		return nil
	}
	c, ok := n.(*ast.CallExpr)
	if !ok {
		return w
	}
	switch fn := c.Fun.(type) {
	case *ast.SelectorExpr:
		recv, ok := fn.X.(*ast.Ident)
		if !ok {
			return w
		}
		if w.ctx.tNames[recv.Name] && failureMethods[fn.Sel.Name] {
			w.failed = true
			return nil
		}
		if testifyPkgs[recv.Name] || recv.Name == "goleak" {
			w.failed = true
			return nil
		}
	}
	return w
}

// testCtx carries the names bound to *testing.T anywhere inside the test
// (outer param plus t.Run closures, which may shadow under another name).
type testCtx struct {
	tNames map[string]bool
}

// classifyTest returns a finding, or nil when the test pins behavior.
func classifyTest(fn *ast.FuncDecl, helpers map[string]bool) *finding {
	ctx := &testCtx{tNames: map[string]bool{}}
	collectTNames(fn.Type, ctx)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		lit, ok := n.(*ast.FuncLit)
		if !ok {
			return true
		}
		collectTNames(lit.Type, ctx)
		return true
	})

	v := &visitor{ctx: ctx, helpers: helpers}
	ast.Walk(v, fn.Body)

	if v.specific > 0 || v.helperSinks > 0 {
		return nil
	}
	if v.failures == 0 {
		return &finding{class: "vacuous"}
	}
	return &finding{class: "err-only"}
}

// collectTNames records parameter names typed *testing.T.
func collectTNames(ft *ast.FuncType, ctx *testCtx) {
	if ft.Params == nil {
		return
	}
	for _, p := range ft.Params.List {
		star, ok := p.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		sel, ok := star.X.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != testingPkg || sel.Sel.Name != "T" {
			continue
		}
		for _, n := range p.Names {
			ctx.tNames[n.Name] = true
		}
	}
}

type visitor struct {
	ctx         *testCtx
	helpers     map[string]bool
	ifStack     []*ast.IfStmt
	failures    int
	specific    int
	helperSinks int
}

func (v *visitor) Visit(n ast.Node) ast.Visitor {
	switch n := n.(type) {
	case *ast.IfStmt:
		v.ifStack = append(v.ifStack, n)
		return &scopedVisitor{parent: v}
	case *ast.CallExpr:
		v.call(n)
	}
	return v
}

// scopedVisitor pops one if-frame when its subtree is done.
type scopedVisitor struct {
	parent *visitor
}

func (s *scopedVisitor) Visit(n ast.Node) ast.Visitor {
	if n == nil {
		s.parent.ifStack = s.parent.ifStack[:len(s.parent.ifStack)-1]
		return nil
	}
	return s.parent.Visit(n)
}

func (v *visitor) call(c *ast.CallExpr) {
	switch fn := c.Fun.(type) {
	case *ast.SelectorExpr:
		recv, ok := fn.X.(*ast.Ident)
		if !ok {
			return
		}
		if v.ctx.tNames[recv.Name] {
			if !failureMethods[fn.Sel.Name] {
				return // t.Skip, t.Log, t.Run, t.Parallel, t.Helper…
			}
			v.failures++
			if !v.errGuarded() {
				v.specific++
			}
			return
		}
		if testifyPkgs[recv.Name] {
			v.failures++
			if !testifyErrOnly[fn.Sel.Name] {
				v.specific++
			}
			return
		}
		if recv.Name == "goleak" {
			v.helperSinks++
			return
		}
		// helper method: f.waitX(t, …) — counts when the method asserts.
		if v.helpers[fn.Sel.Name] {
			v.helperSinks++
		}
	case *ast.Ident:
		// helper func: waitX(t, …) — counts when the helper asserts.
		if v.helpers[fn.Name] {
			v.helperSinks++
		}
	}
}

// errGuarded reports whether the innermost enclosing if guards on `err != nil`
// — the plumbing shape. `err == nil` (want-error) pins the failure mode and
// counts as specific. Any other condition (status checks, string compares) is
// specific.
func (v *visitor) errGuarded() bool {
	if len(v.ifStack) == 0 {
		return false
	}
	cond := v.ifStack[len(v.ifStack)-1].Cond
	bin, ok := cond.(*ast.BinaryExpr)
	if !ok {
		return false
	}
	if bin.Op != token.NEQ {
		return false
	}
	return mentionsIdent(bin.X, "err") || mentionsIdent(bin.Y, "err")
}

func mentionsIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}
