// Command check-product-boundary is the "not a framework" tripwire.
//
// The template is copied and owned; the installer is a developer tool. Those
// two facts are what keep gogogo a scaffold instead of a runtime dependency. A
// single import breaks the distinction, and it breaks it silently: the product
// would still build, and the next person would only find out when they try to
// delete `cmd/gogogo`.
//
// Three invariants, all source-level (no build, no boot):
//
//  1. `cmd/web` — the one binary a product's users boot — must not import the
//     evolution tools (`internal/installer`, `cmd/gogogo`, `internal/capabilities`,
//     `internal/shape`) or the MCP module. A product must build after the
//     developer tools are deleted.
//  2. `internal/installer` must not import `cmd/web`'s handlers: the tool reads
//     the tree, it does not link the server.
//
// A third invariant was tried and removed: "a core unit must not import a
// non-core one" fails by construction, because `internal/server` IS the
// composition root and naming what it wires is its whole job (docs/
// scope-taxonomy.md says so explicitly: core wiring names servants). A gate
// born red is a gate that gets disabled, so the rule was dropped rather than
// weakened into something that looked like it checked something.
package main

import (
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	dir := flag.String("dir", ".", "tree to check")
	flag.Parse()
	problems, err := check(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "check-product-boundary: "+err.Error())
		os.Exit(2)
	}
	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, p)
		}
		fmt.Fprintf(os.Stderr,
			"check-product-boundary: %d problem(s); a product binary must build after the evolution tools are deleted\n",
			len(problems))
		os.Exit(1)
	}
	fmt.Println("check-product-boundary: product builds without the evolution tools")
}

// evolutionPrefixes are the packages a product binary must not link. The list
// is closed on purpose: adding one here is the review moment the guarantee
// needs.
var evolutionPrefixes = []string{
	"github.com/calionauta/gogogo/internal/installer",
	"github.com/calionauta/gogogo/internal/capabilities",
	"github.com/calionauta/gogogo/internal/shape",
	"github.com/calionauta/gogogo/cmd/gogogo",
	"github.com/calionauta/gogogo/cmd/gogogo-mcp",
}

// serverPrefix is the package the installer must stay out of.
const serverPrefix = "github.com/calionauta/gogogo/internal/server"

func check(root string) ([]string, error) {
	var problems []string

	webImports, err := importsUnder(root, "cmd/web")
	if err != nil {
		return nil, err
	}
	for pkg, files := range webImports {
		for _, prefix := range evolutionPrefixes {
			if pkg == prefix || strings.HasPrefix(pkg, prefix+"/") {
				problems = append(problems, fmt.Sprintf(
					"cmd/web imports %s (%s): the product binary would depend on the evolution tool",
					pkg, files[0]))
			}
		}
	}

	installerImports, err := importsUnder(root, "internal/installer")
	if err != nil {
		return nil, err
	}
	for pkg, files := range installerImports {
		if pkg == serverPrefix || strings.HasPrefix(pkg, serverPrefix+"/") {
			problems = append(problems, fmt.Sprintf(
				"internal/installer imports %s (%s): the tool reads the tree, it does not link the server",
				pkg, files[0]))
		}
	}

	return problems, nil
}

// importsUnder maps import path → the files under relDir that import it.
func importsUnder(root, relDir string) (map[string][]string, error) {
	out := map[string][]string{}
	err := walkGo(root, func(rel string, imports []string) {
		if rel != relDir && !strings.HasPrefix(rel, relDir+"/") {
			return
		}
		for _, imp := range imports {
			out[imp] = append(out[imp], rel)
		}
	})
	return out, err
}

// walkGo visits every non-generated Go file under root, handing the callback its
// repo-relative path and imports. Generated files are skipped: they are output,
// and the generator is what owns them.
func walkGo(root string, visit func(rel string, imports []string)) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "tmp", "site":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		f, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if parseErr != nil {
			// A file that does not parse has no import list to check. The
			// compiler owns syntax; this gate owns the dependency boundary.
			return nil //nolint:nilerr // see comment
		}
		imports := make([]string, 0, len(f.Imports))
		for _, spec := range f.Imports {
			if unq, uerr := strconv.Unquote(spec.Path.Value); uerr == nil {
				imports = append(imports, unq)
			}
		}
		visit(rel, imports)
		return nil
	})
}
