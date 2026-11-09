package installer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/calionauta/gogogo/internal/recipe"
)

// recipeCheckComplexity is a small, deterministic source profile.
// Generated _templ.go files are excluded: their AST shape belongs to
// the component, not to the recipe's hand-written wiring.
type recipeCheckComplexity struct {
	GoFiles          int    `json:"goFiles"`
	GoLines          int    `json:"goLines"`
	LargestFunction  string `json:"largestFunction,omitempty"`
	LargestFuncLines int    `json:"largestFuncLines,omitempty"`
	LargestFile      string `json:"largestFile,omitempty"`
	LargestFileLines int    `json:"largestFileLines,omitempty"`
	GoTestFiles      int    `json:"goTestFiles"`
	BranchPoints     int    `json:"branchPoints,omitempty"`
}

// checkRecipeSource profiles the installed Go files from the receipt. It
// only parses; it never runs recipe code.
func checkRecipeSource(root string, receipt recipe.Receipt, report RecipeCheckReport) (
	RecipeCheckReport, recipeCheckComplexity,
) {
	cx := recipeCheckComplexity{}
	for rel := range receipt.Files {
		if !strings.HasSuffix(rel, ".go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		fileLines := strings.Count(string(raw), "\n") + 1
		cx.GoLines += fileLines
		if strings.HasSuffix(rel, "_test.go") {
			cx.GoTestFiles++
			continue
		}
		if strings.HasSuffix(rel, "_templ.go") {
			continue
		}
		cx.GoFiles++
		if fileLines > cx.LargestFileLines {
			cx.LargestFileLines = fileLines
			cx.LargestFile = rel
		}
		cx = appendSourceComplexity(cx, raw, rel)
	}
	return report, cx
}

func appendSourceComplexity(cx recipeCheckComplexity, raw []byte, rel string) recipeCheckComplexity {
	fset := token.NewFileSet()
	tree, err := parser.ParseFile(fset, rel, raw, parser.AllErrors)
	if err != nil {
		return cx
	}
	for _, decl := range tree.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		start := fset.Position(fn.Body.Lbrace).Line
		end := fset.Position(fn.Body.Rbrace).Line
		lines := end - start + 1
		if lines > cx.LargestFuncLines {
			cx.LargestFuncLines = lines
			cx.LargestFunction = fn.Name.Name
		}
		cx.BranchPoints += funcBranchPoints(fn)
	}
	return cx
}

// funcBranchPoints is a deliberately small cyclomatic approximation:
// every control-flow keyword in the function adds one.
func funcBranchPoints(fn *ast.FuncDecl) int {
	n := 0
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch x := node.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
			n++
		case *ast.BinaryExpr:
			if x.Op == token.LAND || x.Op == token.LOR {
				n++
			}
		}
		return true
	})
	return n
}
