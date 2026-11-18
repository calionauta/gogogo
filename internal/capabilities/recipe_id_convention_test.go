// The recipe-id convention, enforced.
//
// Every in-tree feature declares the id its schema and its recipe declaration
// are applied under. There is one spelling of that declaration and one place it
// lives, and this test is what keeps a future feature from inventing a third:
//
//	const RecipeID = "<literal>"     in features/<name>/handlers/
//
// Why a convention gate at all: the id is not a free-floating string. It is the
// namespace a feature's DDL is applied under (`dbschema.MigrationSet.ID`), the
// name its declaration reports over MCP, and the key its receipt in `.recipes/`
// is filed under. Three consumers, so a fourth spelling is silent corruption
// rather than a compile error — the ledger writes under one name and the
// declaration answers to another.
//
// It exists because that exact drift shipped. `RecipeID` was briefly moved out of
// features/todo/handlers and into features/todo, on the reasoning that the
// native shell needed the id without importing the handler tree. That reasoning
// was sound about the SYMPTOM (cmd/gui must not link cgo) and wrong about the
// cure: it made todo the one feature in twelve whose constant lived somewhere
// else, and a per-feature exception is precisely the class of change this repo's
// boundary gates exist to refuse. cmd/gui now spells the id as a local literal
// and pins it against the unit in a test — see cmd/gui/main_test.go — which
// keeps the convention AND the cgo boundary.
package capabilities_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// recipeIDName is the single spelling. A feature that needs the id under a
// different name is a feature that has not read this test.
const recipeIDName = "RecipeID"

// featureHandlers matches every feature's handler package directory. A feature
// with no handlers directory declares no surface and is out of scope.
const featureHandlers = "features/*/handlers"

func TestRecipeIDConvention(t *testing.T) {
	root := repoRoot(t)

	dirs := map[string]bool{}
	matches, err := filepath.Glob(filepath.Join(root, featureHandlers))
	if err != nil {
		t.Fatalf("glob %s: %v", featureHandlers, err)
	}
	for _, d := range matches {
		dirs[d] = true
	}
	if len(dirs) == 0 {
		t.Fatalf("no %s directories found under %s", featureHandlers, root)
	}

	// declared[id] = the package path that declared it, so a duplicate id
	// names both offenders instead of just the second one found.
	declared := map[string]string{}
	var missing []string

	for dir := range dirs {
		feature := filepath.Base(filepath.Dir(dir))
		pkg := readPackage(t, dir)
		if pkg == nil {
			continue
		}

		decls := findConstString(pkg, recipeIDName)
		switch len(decls) {
		case 0:
			// A handlers package that declares a recipe surface must name
			// itself. `Register:` in internal/capabilities is that marker.
			if registersFeature(t, root, feature) {
				missing = append(missing, feature)
			}
		case 1:
			entry := decls[0]
			value, lit := entry.value, entry.literal
			if !lit {
				t.Errorf("%s: %s is not a string literal; the convention is\n"+
					"\tconst %s = \"...\"\n"+
					"so the id can be read without evaluating anything.",
					feature, recipeIDName, recipeIDName)
				continue
			}
			if want := featureID(t, root, feature); want != "" && want != value {
				t.Errorf("%s: %s = %q but .recipes/%s.yml declares id: %q.\n"+
					"The constant, the declaration and the receipt must agree — "+
					"that id is the namespace the feature's DDL is applied under.",
					feature, recipeIDName, value, feature, want)
			}
			if prev, dup := declared[value]; dup {
				t.Errorf("id %q is declared by both %s and %s", value, prev, feature)
			}
			declared[value] = feature
		default:
			t.Errorf("%s declares %s %d times; one feature declares one id",
				feature, recipeIDName, len(decls))
		}
	}

	sort.Strings(missing)
	for _, feature := range missing {
		t.Errorf("%s registers a recipe surface but declares no %s in its handlers package.\n"+
			"Add, beside the declaration it names:\n"+
			"\tconst %s = %q",
			feature, recipeIDName, recipeIDName, feature)
	}

	// The other direction: the constant must not live anywhere else. This is the
	// half that would have caught the features/todo detour.
	if strays := findStrayDeclarations(t, root, dirs); len(strays) > 0 {
		sort.Strings(strays)
		for _, s := range strays {
			t.Errorf("%s declares %s outside a feature's handlers package.\n"+
				"The id belongs beside the declaration that reports it (features/<name>/handlers).\n"+
				"If a non-handler caller needs the id, give it a local literal and pin it\n"+
				"against the unit in a test — do not move the constant.",
				s, recipeIDName)
		}
	}
}

// repoRoot walks up from the test's working directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s", dir)
		}
		dir = parent
	}
}

// parsedPkg is a directory's non-test Go files. A handlers package spreads its
// declaration over several files (recipe.go, chi.go, register.go), so the search
// has to cover all of them — reading only the first is how this gate first
// reported seven false "declares no RecipeID".
type parsedPkg struct {
	files []*ast.File
}

// readPackage parses every non-test Go file in one directory.
func readPackage(t *testing.T, dir string) *parsedPkg {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	pkg := &parsedPkg{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		pkg.files = append(pkg.files, f)
	}
	return pkg
}

// constDecl is one declaration of the constant: its value (when it is a string
// literal) and whether it was one.
type constDecl struct {
	value   string
	literal bool
}

// findConstString returns every declaration of name and whether its value is a
// string literal. A non-literal declaration is reported rather than evaluated:
// this gate reads source, it does not type-check.
func findConstString(pkg *parsedPkg, name string) []constDecl {
	var out []constDecl
	for _, file := range pkg.files {
		for _, d := range file.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, n := range vs.Names {
					if n.Name != name {
						continue
					}
					value, lit := "", false
					if i < len(vs.Values) {
						if bl, ok := vs.Values[i].(*ast.BasicLit); ok && bl.Kind == token.STRING {
							if unq, err := strconv.Unquote(bl.Value); err == nil {
								value, lit = unq, true
							}
						}
					}
					out = append(out, constDecl{value: value, literal: lit})
				}
			}
		}
	}
	return out
}

// featureID reads the id a feature's receipt declares, so the constant and the
// receipt cannot drift. A feature with no receipt returns "" (nothing to check).
func featureID(t *testing.T, root, feature string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".recipes", feature+".yml"))
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "id:"); ok {
			return strings.Trim(strings.TrimSpace(rest), `"'`)
		}
	}
	return ""
}

// registersFeature reports whether internal/capabilities lists a Register path
// for this feature — the marker that it declares a surface (and so must name
// itself). Read as text on purpose: importing the registry here would make this
// test depend on the thing it is checking.
func registersFeature(t *testing.T, root, feature string) bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "internal", "capabilities", "registry.go"))
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	return strings.Contains(string(raw), `"features/`+feature+`/handlers"`)
}

// findStrayDeclarations looks for the constant anywhere in the tree that is not
// a feature's handlers package. It skips the site build output and vendor.
func findStrayDeclarations(t *testing.T, root string, allowed map[string]bool) []string {
	t.Helper()
	var strays []string
	skip := map[string]bool{"vendor": true, "node_modules": true, ".git": true, "dist": true}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			// A disappeared file mid-walk is not a convention violation; the
			// walk continues. Bailing out would make the gate flaky under
			// `go test -race` on a tree another process is writing.
			return nil //nolint:nilerr // see above
		}
		if d.IsDir() {
			if skip[d.Name()] {
				return filepath.SkipDir
			}
			if allowed[path] {
				// The convention's home: not a stray. Keep walking into it
				// anyway so a nested duplicate is still caught.
				return nil
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		dir := filepath.Dir(path)
		if allowed[dir] {
			return nil
		}
		// The path comes from the walk of the repository root, and this gate only
		// ever reads files it already listed; a symlink swapped mid-walk would
		// have to point at another .go file that declares the constant, which is
		// the same finding. Root-scoped io is not worth it for a test-only read.
		src, rerr := os.ReadFile(path) //nolint:gosec // see above
		if rerr != nil {
			return nil //nolint:nilerr // a vanished file is not a violation
		}
		if !strings.Contains(string(src), "const "+recipeIDName+" =") {
			return nil
		}
		// A scaffold's write-as-template is the convention being EMITTED, not an
		// exception to it: the generated tree gets the constant in the generated
		// feature's handlers package. Recognise it by the module placeholder
		// every scaffold template carries.
		if strings.Contains(string(src), `"{{module}}/`) {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		strays = append(strays, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return strays
}
