package gorules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// TestNoLegacyTopLevelIdents enforces AGENTS.md "NO legacy until 1.0" in
// CI instead of prose: pre-1.0 the template keeps no backward-compat
// shims, so no top-level Go identifier may start with legacy/Legacy.
// (A ruleguard rule was tried first and reverted: declaration-position
// captures don't filter by name in this engine version — the fixture
// proved 0 hits on real shims. An AST walk states the same rule with no
// engine between the code and the verdict.)
//
// Scope mirrors the rule it replaces: hand-written non-test Go only.
// Generated code (*_templ.go, sqlcgen), tests, SQL migrations and prose
// (comments, decision records, CHANGELOG) are out of scope by
// construction — the walk never reads them.
func TestNoLegacyTopLevelIdents(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(file), "..")
	hits := make([]string, 0, 4)
	for _, tree := range []string{"internal", "features", "config", "cmd"} {
		hits = append(hits, scanTreeForLegacy(t, filepath.Join(root, tree))...)
	}
	if len(hits) > 0 {
		t.Errorf("legacy-prefixed top-level identifiers (delete the old path, do not bridge it):\n  %s",
			strings.Join(hits, "\n  "))
	}
}

var legacyIdentRE = regexp.MustCompile(`^(?i)legacy`)

// scanTreeForLegacy returns file:line:name hits under root, skipping test
// files and generated code. Paths are collected inside the walk and read
// outside it: gosec G122 forbids filesystem operations on walked paths
// inside the callback (symlink TOCTOU); os.Root scoping is overkill for a
// test walking its own checkout, so collect-then-read is the shape.
func scanTreeForLegacy(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") ||
			strings.HasSuffix(name, "_templ.go") {
			return nil
		}
		if strings.Contains(filepath.ToSlash(path), "/sqlcgen/") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	hits := make([]string, 0, len(paths))
	for _, path := range paths {
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("read %s: %v", path, rerr)
		}
		for _, h := range legacyIdentsInSrc(raw) {
			hits = append(hits, path+":"+h)
		}
	}
	slices.Sort(hits)
	return hits
}

// legacyIdentsInSrc reports line:name for top-level func/type/var/const
// identifiers starting with legacy (any case). Pure function of source so
// the contract is provable without a repo checkout (see below).
func legacyIdentsInSrc(raw []byte) []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "src.go", raw, parser.SkipObjectResolution)
	if err != nil {
		return []string{"unparseable"}
	}
	var hits []string
	add := func(id *ast.Ident) {
		if id != nil && legacyIdentRE.MatchString(id.Name) {
			hits = append(hits, fset.Position(id.Pos()).String()+":"+id.Name)
		}
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			add(d.Name)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					add(s.Name)
				case *ast.ValueSpec:
					for _, id := range s.Names {
						add(id)
					}
				}
			}
		}
	}
	return hits
}

// TestLegacyIdentsInSrcProvesTheRule is the red-proof: the shapes that must
// fire do, and ordinary code (including legacy mentions in prose, which the
// walk never reads) stays quiet. Delete the matcher and this fails.
func TestLegacyIdentsInSrcProvesTheRule(t *testing.T) {
	fire := `package x

func legacySubject(p string) string { return p }
func (w widget) legacyRender() {}
type LegacyWidget struct{ N int }
var legacyFlag = true
const legacyMode = 1
`
	got := legacyIdentsInSrc([]byte(fire))
	if len(got) != 5 {
		t.Fatalf("hits = %v, want 5 (func, method, type, var, const)", got)
	}
	quiet := `package x

// legacyOAuthSubject was deleted with the rule, not bridged.
func delegatedWork() bool { return true }
func currentSubject(p string) string { return p }
`
	if got := legacyIdentsInSrc([]byte(quiet)); len(got) != 0 {
		t.Fatalf("hits = %v, want none (prose is invisible, delegate is no prefix)", got)
	}
}
