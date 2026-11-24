// Credit uses found in files the type loader did not see. See
// markUnloadedUses for why this path exists and why it is fail-open.
package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// markUnloadedUses credits uses found in .go files the type loader did not
// see: build-tag excluded for this GOOS/GOARCH, and every `_test.go` (the
// loader ran without Tests).
//
// Two shapes must be read, because a test file uses an export in two ways:
//
//   - through a qualified selector (`nats.StartEmbedded`), from another
//     package's test;
//   - unqualified (`LoadUnpublished()`), from the owner package's OWN test —
//     and this is the common case, so reading only selectors silently reported
//     every tested-but-unused export as dead.
func markUnloadedUses(dir string, decls map[string]*finding, loaded map[string]bool) error {
	files, err := unloadedGoFiles(dir, loaded)
	if err != nil {
		return err
	}

	fset := token.NewFileSet()
	for _, filePath := range files {
		// A file that mentions no module path cannot credit anything: every use
		// this pass recognizes is either a selector on an in-module import or an
		// unqualified name in its own package's test, and both need the module
		// path to resolve. Reading 500-odd test files just to parse them was the
		// bulk of this gate's cost; a byte scan is ~100x cheaper.
		if !mentionsModule(filePath, dir) {
			continue
		}
		f, perr := parser.ParseFile(fset, filePath, nil, parser.SkipObjectResolution)
		if perr != nil || ast.IsGenerated(f) {
			continue // not this gate's business: unparseable, or not hand-written
		}
		// The file decides the class, not the package: demoting every use in a
		// package that has any test would be wrong the other way round.
		isTest := strings.HasSuffix(filePath, "_test.go")
		alias := moduleAliases(f)
		ownPkg := ownPackagePath(dir, filePath, f.Name.Name)
		credit := func(name string) {
			f, ok := decls[name]
			if !ok {
				return
			}
			if isTest {
				f.testUses++
				return
			}
			f.extUses++
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				id, ok := x.X.(*ast.Ident)
				if !ok {
					return true
				}
				if p, ok := alias[id.Name]; ok {
					credit(p + "." + x.Sel.Name)
				}
			case *ast.Ident:
				if ownPkg != "" {
					credit(ownPkg + "." + x.Name)
				}
			}
			return true
		})
	}
	return nil
}

// exportedKind is the kinds this gate judges. Methods are skipped: their
// consumer is an interface, and an interface method is called through the
// interface, not by name — `iface` answers that question. Fields are skipped
// for the same reason.
//
// Internal visibility is not judged either: Go's `internal/` rule already
// means "this module only", so an export there is still a cross-package
// promise, just a smaller one.
func exportedKind(obj types.Object) (string, bool) {
	switch o := obj.(type) {
	case *types.Func:
		if sig, ok := o.Type().(*types.Signature); ok && sig.Recv() != nil {
			return "", false
		}
		return "func", true
	case *types.TypeName:
		return "type", true
	case *types.Var:
		if o.IsField() {
			return "", false
		}
		return "var", true
	case *types.Const:
		return "const", true
	default:
		return "", false
	}
}

// display trims the module prefix, so a finding reads like the allowlist entry
// a reviewer has to add or the file they have to open.
func display(name string) string { return strings.TrimPrefix(name, modulePath+"/") }

// unloadedGoFiles lists the .go files the type loader did not see: those it
// never compiled (build-tag excluded on this host) and every `_test.go`.
func unloadedGoFiles(dir string, loaded map[string]bool) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			// testdata is skipped by the Go tool itself, so nothing there is a
			// package this gate could judge — and the fixtures it holds are
			// throwaway modules with their own import paths. Reading them would
			// credit selectors against paths no declaration has.
			if path != dir && (name == "node_modules" || name == "vendor" ||
				name == "testdata" || strings.HasPrefix(name, ".") || name == "tmp") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(name, ".go") && !loaded[path] {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// ownPackagePath is the import path the file itself belongs to, which is what
// an UNQUALIFIED identifier in it resolves against. Empty for an external test
// package (`foo_test`): it declares no exports of its own, so nothing there can
// be credited to the package under test.
func ownPackagePath(dir, filePath, pkgName string) string {
	if strings.HasSuffix(pkgName, "_test") {
		return ""
	}
	rel, err := filepath.Rel(dir, filepath.Dir(filePath))
	if err != nil {
		return ""
	}
	if rel == "." {
		return modulePath
	}
	return modulePath + "/" + filepath.ToSlash(rel)
}

// moduleAliases maps the local name an import binds to its in-module import
// path. Only this module's packages are interesting: a selector on a stdlib or
// third-party name can never credit one of our declarations.
func moduleAliases(f *ast.File) map[string]string {
	alias := make(map[string]string, len(f.Imports))
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || !strings.HasPrefix(p, modulePath) {
			continue
		}
		local := path.Base(p)
		if imp.Name != nil {
			local = imp.Name.Name
		}
		alias[local] = p
	}
	return alias
}

// mentionsModule is the cheap pre-filter before a parse: can this file credit
// any declaration at all?
//
// Two use shapes need a parse, and each has a textual signature:
//
//   - a QUALIFIED use needs the module import path somewhere in the text;
//   - an UNQUALIFIED use comes from an in-package test (`package foo`), which
//     names declarations with no path at all — so every such test is a
//     candidate regardless of its text.
//
// Only a file that declares an EXTERNAL test package (`package foo_test`) and
// mentions no module path is hopeless: it cannot credit an unqualified name,
// and it imports nothing of ours to credit a qualified one.
//
// This filter exists because parsing ~520 test files was the bulk of the gate's
// cost. Getting it wrong is expensive in the other direction: an earlier
// version skipped in-package tests and reported 13 already-credited exports as
// dead.
func mentionsModule(filePath string, dir string) bool {
	_ = dir
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return true // cannot tell: let the parser decide rather than skip
	}
	if bytes.Contains(raw, []byte(modulePath)) {
		return true
	}
	return !hasExternalTestPackage(raw)
}

// hasExternalTestPackage reports whether the file's package clause is
// `package foo_test`, which declares no exports of its own.
func hasExternalTestPackage(raw []byte) bool {
	for line := range strings.SplitSeq(string(raw), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "package ")
		if !ok {
			continue
		}
		return strings.HasSuffix(strings.TrimSpace(rest), "_test")
	}
	return false
}
