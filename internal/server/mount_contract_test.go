package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// The property the signature change buys: a unit cannot put routes on the tree
// without handing over a Declaration. Before, MountFeature(owner, router) let a
// unit mount itself with nothing to grade, which is how license and onboarding
// stayed invisible to the headless rule while serving real mutations.
//
// This test reads the call sites rather than the types, because the types are
// already enforced by the compiler: what needs watching is the ESCAPE HATCH.
// Every non-MountInfra mount must pass an expression that reaches a Provider
// (`.Recipe()` or a literal provider), so "call the router directly" has no
// shape left to take.
func TestEveryMountPassesAProvider(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	var checked int
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "MountInfra":
				return true // covered by TestMountInfraHasOnlyTheDeclaredReasons
			case "MountFeature":
			default:
				return true
			}
			checked++
			if len(call.Args) != 1 {
				t.Errorf("%s: MountFeature with %d args, want exactly one Provider",
					fset.Position(call.Pos()), len(call.Args))
				return true
			}
			if !reachesAProvider(call.Args[0]) {
				t.Errorf("%s: MountFeature argument is not a Provider — mount a unit's "+
					"Declaration, or use MountInfra and name the reason",
					fset.Position(call.Pos()))
			}
			return true
		})
	}
	if checked == 0 {
		t.Fatal("no MountFeature call sites found: this test is no longer looking at the tree")
	}
}

// reachesAProvider is the shape test: `.Recipe()` or a provider literal. It is
// deliberately syntactic — a Provider value cannot be recognised statically — so
// it accepts the two spellings the tree uses and flags anything else for review.
func reachesAProvider(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.CallExpr:
		sel, ok := v.Fun.(*ast.SelectorExpr)
		return ok && sel.Sel.Name == "Recipe"
	case *ast.CompositeLit:
		return true
	}
	return false
}
