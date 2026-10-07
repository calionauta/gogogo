// SCOPE:layer=infra,removal=plugin — installer engine: trim import-coverage test.
package installer

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// TestTrimUnitsCoverImports generalizes the registry's dagnats import guard
// to every manifest unit: trimming deletes a unit's Dirs and Files, so any
// file outside them that imports a deleted package breaks the proof build
// with a dangling import. Sibling dirs of the same unit (engine dir +
// demo feature dir) go together — the check groups by UNIT, not by
// capability, because the unit is what trim deletes.
func TestTrimUnitsCoverImports(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("runtime.Caller unavailable")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..")

	for _, u := range manifestUnits {
		t.Run(u.id, func(t *testing.T) {
			checkUnitTrimImports(t, root, u)
		})
	}
}

// checkUnitTrimImports holds one unit's walk so the top test stays under
// the gocognit budget.
func checkUnitTrimImports(t *testing.T, root string, u trimUnit) {
	t.Helper()
	m := u.meta()
	var pkgs []string
	for _, d := range m.dirs {
		if strings.HasPrefix(d, "internal/") || strings.HasPrefix(d, "features/") {
			pkgs = append(pkgs, "github.com/calionauta/gogogo/"+d)
		}
	}
	if len(pkgs) == 0 {
		t.Skipf("unit %q owns no importable package", u.id)
	}
	var offenders []string
	for _, tree := range []string{"cmd", "features", "internal", "router"} {
		walkErr := filepath.WalkDir(filepath.Join(root, tree), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			// Generated Templ output regenerates from the covered
			// .templ sources on trim (the installer re-runs
			// generate) — same exclusion as the file-sizes gate.
			if strings.HasSuffix(path, "_templ.go") {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			if unitCoversPath(u, m, rel) {
				return nil
			}
			hits, perr := fileImportsOwn(path, pkgs)
			if perr != nil {
				return perr
			}
			for _, h := range hits {
				offenders = append(offenders, rel+" imports "+h)
			}
			return nil
		})
		if walkErr != nil {
			t.Fatal(walkErr)
		}
	}
	if len(offenders) > 0 {
		t.Errorf("unit %q trim leaves dangling imports:\n  %s", u.id, strings.Join(offenders, "\n  "))
	}
}

// unitCoversPath reports whether trim deletes (dirs, files) or edits
// (any strip/drop/replace rule names the path — the rule itself removes
// the import, e.g. desktop drop lines and templ call-site drops) a file.
func unitCoversPath(u trimUnit, m unitMeta, rel string) bool {
	for _, d := range m.dirs {
		if rel == d || strings.HasPrefix(rel, d+"/") {
			return true
		}
	}
	if slices.Contains(m.files, rel) {
		return true
	}
	for _, es := range u.extraStrips {
		if rel == es.path {
			return true
		}
	}
	for _, ed := range u.extraDrops {
		if rel == ed.path {
			return true
		}
	}
	for _, r := range u.replaces {
		if rel == r.path {
			return true
		}
	}
	if len(u.desktopStrips) > 0 || len(u.desktopDropLines) > 0 {
		return rel == "cmd/desktop/main.go"
	}
	return false
}

// fileImportsOwn returns the subset of imports that name one of pkgs.
// Real imports (parser), not substrings: comments and string literals
// naming a package are not dependencies.
func fileImportsOwn(path string, pkgs []string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	var hits []string
	for _, imp := range f.Imports {
		quoted := strings.Trim(imp.Path.Value, `"`)
		if slices.Contains(pkgs, quoted) {
			hits = append(hits, quoted)
		}
	}
	return hits, nil
}
