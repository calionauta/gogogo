// SCOPE:core — Static sink guard for the HTTP handler surface.
//
// Ruleguard covers a few shapes, but it cannot see a request-derived URL
// inside an http.Get or a path inside os.Open. This test makes those sinks
// fail review instead of shipping quietly: the handler packages must not
// contain outbound HTTP, command execution, raw filesystem opens or
// templ.Raw. A legitimate need belongs in a named helper with its own
// validation and an explicit exception here.
package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// dangerousSink is one package-qualified function name that must not appear
// in a handler package.
var dangerousSink = map[string]bool{
	"exec.Command":               true,
	"exec.CommandContext":        true,
	"http.Get":                   true,
	"http.Post":                  true,
	"http.NewRequest":            true,
	"http.NewRequestWithContext": true,
	"os.Open":                    true,
	"os.OpenFile":                true,
	"os.ReadFile":                true,
	"os.WriteFile":               true,
	"os.Remove":                  true,
	"os.RemoveAll":               true,
	"templ.Raw":                  true,
	"template.HTML":              true,
}

func TestHandlerSurfaceHasNoRawSinks(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	handlerDirs, err := filepath.Glob(filepath.Join(root, "features", "*", "handlers"))
	if err != nil {
		t.Fatalf("glob handlers: %v", err)
	}
	files := make([]string, 0, len(handlerDirs)*8+8)
	for _, dir := range handlerDirs {
		files = append(files, goFilesUnder(t, dir)...)
	}
	files = append(files, goFilesUnder(t, filepath.Join(root, "internal", "server"))...)

	fset := token.NewFileSet()
	for _, path := range files {
		astFile, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", path, perr)
		}
		ast.Inspect(astFile, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := qualifiedCall(call)
			if !dangerousSink[name] {
				return true
			}
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s:%d calls %s — put the sink behind a validated helper or add a reviewed exception",
				rel, fset.Position(call.Pos()).Line, name)
			return true
		})
	}
}

// goFilesUnder returns non-test, non-generated Go files in dir.
func goFilesUnder(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, "_templ.go") {
			return nil
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return out
}

// qualifiedCall renders pkg.Func for selector calls, "" otherwise.
func qualifiedCall(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return pkg.Name + "." + sel.Sel.Name
}
