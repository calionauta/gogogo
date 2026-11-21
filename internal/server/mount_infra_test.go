package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// MountInfra is the escape hatch from "every mount declares its surface". An
// escape hatch nobody watches becomes the norm, so this test pins WHO uses it
// and WHY: each call site must name a reason, and the set of owners is asserted
// exactly. Adding a third means editing this list, which is the review.
func TestMountInfraHasOnlyTheDeclaredReasons(t *testing.T) {
	want := map[string]string{
		"auth-pages": "authorization server",
		"perf":       "developer tooling",
	}

	got := map[string]string{}
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
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
			if !ok || sel.Sel.Name != "MountInfra" {
				return true
			}
			if len(call.Args) != 3 {
				t.Errorf("%s: MountInfra with %d args, want (owner, reason, router)", name, len(call.Args))
				return true
			}
			owner := literalString(call.Args[0])
			if owner == "" {
				t.Errorf("%s: MountInfra owner is not a literal", name)
				return true
			}
			reason := literalString(call.Args[1])
			if strings.TrimSpace(reason) == "" {
				t.Errorf("%s: MountInfra(%q) has no reason", name, owner)
				return true
			}
			got[owner] = reason
			return true
		})
	}

	for owner, wantSub := range want {
		reason, ok := got[owner]
		if !ok {
			t.Errorf("MountInfra(%q) is gone: if the unit now declares, update this list", owner)
			continue
		}
		if !strings.Contains(reason, wantSub) {
			t.Errorf("MountInfra(%q) reason = %q, want it to name %q", owner, reason, wantSub)
		}
	}
	for owner := range got {
		if _, ok := want[owner]; !ok {
			t.Errorf("MountInfra(%q) is a NEW escape hatch: declare the unit instead, "+
				"or add it here with the reason it cannot", owner)
		}
	}
}

// literalString renders a string expression made only of literals (a plain
// string or a `+` concatenation across lines), so a caller can wrap a long
// reason without defeating the check. A computed value returns "".
func literalString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return ""
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return ""
		}
		return s
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return ""
		}
		left, right := literalString(v.X), literalString(v.Y)
		if left == "" && right == "" {
			return ""
		}
		return left + right
	case *ast.ParenExpr:
		return literalString(v.X)
	}
	return ""
}
