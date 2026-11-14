// Command check-typed-states is the "no stringly-typed state" gate.
//
// A named string type (RecipeState, FileState, Layer, Kind…) exists so the
// spelling of a state is decided once. Comparing one against a bare literal
// (`if d.State == "removed"`) throws that away: a typo compiles, the switch
// silently falls through to the default, and the constant nobody used drifts
// from the literal nobody checks. The compiler cannot see it because every
// literal is an untyped constant that converts silently.
//
// This gate reads TYPES, not text: a named type declared in this module whose
// underlying type is string, compared with a string literal in an equality or a
// switch case. Construction is deliberately not flagged — `const StateX T =
// "x"` is where the literal is supposed to live, and a zero-value literal is
// not a comparison.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

const modulePath = "github.com/calionauta/gogogo/"

func main() {
	dir := flag.String("dir", ".", "module root to check")
	flag.Parse()
	problems, err := check(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "check-typed-states: "+err.Error())
		os.Exit(2)
	}
	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, p)
		}
		fmt.Fprintf(os.Stderr,
			"check-typed-states: %d comparison(s) against a string literal; "+
				"use the type's constants instead\n", len(problems))
		os.Exit(1)
	}
	fmt.Println("check-typed-states: states are compared through their constants")
}

// check loads every package in the module with type information and reports
// named-state comparisons against string literals.
func check(dir string) ([]string, error) {
	cfg := &packages.Config{
		Dir:  dir,
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, pkg := range pkgs {
		if pkg.TypesInfo == nil {
			continue
		}
		for _, file := range pkg.Syntax {
			problems = append(problems, fileProblems(pkg.Fset, pkg.TypesInfo, file)...)
		}
	}
	sort.Strings(problems)
	return problems, nil
}

// fileProblems walks one file with a parent stack, so a switch case can find
// the switch it belongs to (ast has no parent pointers).
func fileProblems(fset *token.FileSet, info *types.Info, file *ast.File) []string {
	var out []string
	var stack []ast.Node
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		switch node := n.(type) {
		case *ast.BinaryExpr:
			if p, ok := comparisonProblem(fset, info, node); ok {
				out = append(out, p)
			}
		case *ast.CaseClause:
			if p, ok := caseProblem(fset, info, stack, node); ok {
				out = append(out, p)
			}
		}
		stack = append(stack, n)
		return true
	})
	return out
}

// comparisonProblem reports an equality between a named state and a literal.
func comparisonProblem(fset *token.FileSet, info *types.Info, node *ast.BinaryExpr) (string, bool) {
	if node.Op != token.EQL && node.Op != token.NEQ {
		return "", false
	}
	for _, pair := range [2][2]ast.Expr{{node.X, node.Y}, {node.Y, node.X}} {
		state, ok := namedStringType(info, pair[0])
		if !ok {
			continue
		}
		if p, ok := literalProblem(fset, state, pair[1]); ok {
			return p, true
		}
	}
	return "", false
}

// caseProblem reports every literal case of a switch whose tag is a state.
func caseProblem(fset *token.FileSet, info *types.Info, stack []ast.Node, clause *ast.CaseClause) (string, bool) {
	sw := enclosingSwitch(stack)
	if sw == nil || sw.Tag == nil {
		return "", false
	}
	state, ok := namedStringType(info, sw.Tag)
	if !ok {
		return "", false
	}
	for _, expr := range clause.List {
		if p, ok := literalProblem(fset, state, expr); ok {
			return p, true
		}
	}
	return "", false
}

// enclosingSwitch finds the nearest switch the stack belongs to.
func enclosingSwitch(stack []ast.Node) *ast.SwitchStmt {
	for _, node := range slices.Backward(stack) {
		if sw, ok := node.(*ast.SwitchStmt); ok {
			return sw
		}
	}
	return nil
}

// literalProblem pairs a known state type with a string literal.
func literalProblem(fset *token.FileSet, state *types.Named, expr ast.Expr) (string, bool) {
	lit, ok := stringLiteral(expr)
	if !ok {
		return "", false
	}
	pos := fset.Position(expr.Pos())
	return fmt.Sprintf(
		"%s:%d: %s compared with %q — use %s's constants (a typo here compiles and falls through)",
		pos.Filename, pos.Line, state.Obj().Name(), lit, state.Obj().Name()), true
}

// stringLiteral returns the value of a string literal expression.
func stringLiteral(expr ast.Expr) (string, bool) {
	basic, ok := expr.(*ast.BasicLit)
	if !ok || basic.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(basic.Value)
	return s, err == nil
}

// namedStringType reports whether expr has a named string type declared in this
// module.
func namedStringType(info *types.Info, expr ast.Expr) (*types.Named, bool) {
	tv, ok := info.Types[expr]
	if !ok {
		return nil, false
	}
	named, ok := tv.Type.(*types.Named)
	if !ok {
		return nil, false
	}
	if !isModuleType(named) {
		return nil, false
	}
	basic, ok := named.Underlying().(*types.Basic)
	if !ok || basic.Kind() != types.String {
		return nil, false
	}
	return named, true
}

// isModuleType keeps the rule on this project's vocabulary: a third-party named
// string (an enum from a library) is not ours to change.
func isModuleType(named *types.Named) bool {
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	return strings.HasPrefix(obj.Pkg().Path()+"/", modulePath)
}
