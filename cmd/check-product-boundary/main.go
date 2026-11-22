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
//
//  2. `internal/installer` must not import `cmd/web`'s handlers: the tool reads
//     the tree, it does not link the server.
//
//  3. Shared infrastructure must not name a FEATURE slice. `internal/server` is
//     the composition root — naming what it wires is its whole job, so it is
//     exempt — but every other package under `internal/` is infrastructure that
//     a shell boots. `internal/desktopboot` used to import `features/todo` to
//     apply one migration set; the effect was that the reference slice could
//     never be dropped from a product, because a servant named it. A shell
//     (cmd/*) MAY import a feature: it is the app.
//
// A different third invariant was tried and removed: "a core unit must not
// import a non-core one" fails by construction, because `internal/server` IS
// the composition root and naming what it wires is its whole job (docs/
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

	"github.com/calionauta/gogogo/internal/capabilities"
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

	problems = append(problems, featureImportsFromInfrastructure(root)...)

	// The other direction: the CONSUMER client must not drag the evolution
	// toolchain, or a product that ships product-mcp still needs the installer
	// to be installed. This is the half the guarantee was missing.
	consumerProblems, err := consumerSurfaceProblems(root)
	if err != nil {
		return nil, err
	}
	problems = append(problems, consumerProblems...)

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

// consumerSurfaceProblems enforces the direction the first invariant missed.
//
// checkProductBoundary answers "can the product build after the evolution tools
// are deleted". It never asked "does the product's own call surface exist, and
// is it free of the toolchain". Both are needed: a tree could satisfy the first
// by having no consumer surface at all, which is the state this rule was born
// from — `recipe invoke` lived inside internal/installer, so a product had to
// run the EVOLUTION binary to call its own routes.
func consumerSurfaceProblems(root string) ([]string, error) {
	var problems []string
	// The consumer PAIR is what a generated product ships: the MCP for an agent
	// that speaks JSON-RPC, the CLI for a shell script or an agent that does
	// not. Both must exist and both must be free of the evolution packages —
	// checked by ROLE (`cmd/*-mcp` and `cmd/product-cli`, which the rename turns
	// into the product's own names), not by the template's spelling.
	for _, surface := range []string{"cmd/product-mcp", "cmd/product-cli"} {
		imports, err := importsUnder(root, surface)
		if err != nil {
			return nil, err
		}
		if len(imports) == 0 {
			problems = append(problems, surface+" is missing: a product must ship "+
				"both consumer surfaces (CLI and MCP), or a caller needs the evolution toolchain")
			continue
		}
		for pkg, files := range imports {
			for _, prefix := range evolutionPrefixes {
				if pkg == prefix || strings.HasPrefix(pkg, prefix+"/") {
					problems = append(problems, fmt.Sprintf(
						"%s imports %s (%s): a consumer surface must not link the evolution tool",
						surface, pkg, files[0]))
				}
			}
		}
	}
	// And the client itself must be innocuous: shared code, no installer.
	clientImports, err := importsUnder(root, "internal/productapi")
	if err != nil {
		return nil, err
	}
	for pkg, files := range clientImports {
		if pkg == serverPrefix || strings.HasPrefix(pkg, serverPrefix+"/") {
			problems = append(problems, fmt.Sprintf(
				"internal/productapi imports %s (%s): the consumer client talks to a "+
					"running product over HTTP, it does not link the server", pkg, files[0]))
		}
		for _, prefix := range evolutionPrefixes {
			if pkg == prefix || strings.HasPrefix(pkg, prefix+"/") {
				problems = append(problems, fmt.Sprintf(
					"internal/productapi imports %s (%s): the consumer client must not read a checkout",
					pkg, files[0]))
			}
		}
	}
	return problems, nil
}

// featurePrefix is where a slice lives. Infrastructure may not name it.
const featurePrefix = "github.com/calionauta/gogogo/features/"

// featureImportsFromInfrastructure reports every `internal/` package (outside
// the composition root) that imports a REMOVABLE feature slice.
//
// The two exemptions are deliberate:
//
//   - `internal/server` builds the mux and the registry, so it names every unit
//     by design.
//   - A directory the registry files under a CORE capability is not a slice you
//     can drop: `features/authviews` is the other half of `auth` (its `Dirs`
//     list both), so the shell may name it without making anything
//     unremovable. The gate reads that class from the registry rather than
//     guessing it from the path — the same source `recipe uninstall` uses.
func featureImportsFromInfrastructure(root string) []string {
	coreDirs := map[string]bool{}
	for _, c := range capabilities.All {
		if c.Kind != capabilities.KindCore {
			continue
		}
		for _, dir := range c.Dirs {
			coreDirs[dir] = true
		}
	}
	var problems []string
	err := walkGo(root, func(rel string, imports []string) {
		if !strings.HasPrefix(rel, "internal/") || strings.HasPrefix(rel, "internal/server/") {
			return
		}
		for _, imp := range imports {
			if !strings.HasPrefix(imp, featurePrefix) {
				continue
			}
			dir := strings.TrimPrefix(imp, featurePrefix)
			if i := strings.IndexByte(dir, '/'); i >= 0 {
				dir = dir[:i]
			}
			if coreDirs["features/"+dir] {
				continue
			}
			problems = append(problems, fmt.Sprintf(
				"%s imports %s: infrastructure must not name a removable slice (a shell may; "+
					"pass it in instead, as cmd/gui passes todo's migration set)", rel, imp))
		}
	})
	if err != nil {
		return []string{"feature boundary scan: " + err.Error()}
	}
	return problems
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
