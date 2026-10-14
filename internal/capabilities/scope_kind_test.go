package capabilities

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestScopeAnnotationsMatchRegistryKind binds the two places a removal class is
// written: the registry Kind (declared the single source of truth for kinds)
// and the per-file SCOPE annotation an agent reads first.
//
// They drifted, and the drift is not cosmetic — a reader got opposite
// instructions depending on which file they opened. internal/components and
// internal/datastar said core while their units say plugin (the docs table
// agreed with the units), and internal/natswire said plugin while its unit says
// core (its reason: chi handlers name those types in signatures even with NATS
// off, so trimming would not compile). The class is one axis, so the file must
// not disagree with the unit that owns it.
//
// Dirs owned by two units accept either class (features/room: goakt, room).
// The walk is recursive: a package's subpackages belong to the same unit
// (features/genui/handlers is genui), and that is where the deepest drift hid
// (features/genui's five Go files said feature while its unit says plugin).
func TestScopeAnnotationsMatchRegistryKind(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	trimmed := trimmedPaths(root)
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rootFS.Close() }()

	declared := map[string]map[string]bool{}
	for _, c := range All {
		for _, d := range c.Dirs {
			if strings.Count(d, "/") != 1 || (!strings.HasPrefix(d, "internal/") && !strings.HasPrefix(d, "features/")) {
				continue
			}
			if declared[d] == nil {
				declared[d] = map[string]bool{}
			}
			declared[d][string(c.Kind)] = true
		}
	}

	for pkg, want := range declared {
		dir := filepath.Join(root, filepath.FromSlash(pkg))
		if _, err := os.Stat(dir); err != nil {
			if trimmed[filepath.FromSlash(pkg)] {
				continue
			}
			t.Fatalf("stat %s: %v", pkg, err)
		}
		checkScopeClasses(t, rootFS, root, pkg, want)
	}
}

// checkScopeClasses asserts every hand-written Go file under a unit's package
// carries a class the unit declares. Generated files are skipped by their header
// (sqlcgen, *_templ.go) and tests by suffix; a missing annotation is
// cmd/check-scope's contract, not this test's.
func checkScopeClasses(t *testing.T, rootFS *os.Root, root, pkg string, want map[string]bool) {
	t.Helper()
	scopeRE := regexp.MustCompile(`(?m)^// SCOPE:(\w+)`)
	classes := make([]string, 0, len(want))
	for k := range want {
		classes = append(classes, k)
	}
	slices.Sort(classes)
	dir := filepath.Join(root, filepath.FromSlash(pkg))
	walkErr := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.HasSuffix(p, "_templ.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		// root-scoped read (os.Root), not os.ReadFile(p): gosec G122 flags the
		// path-based form inside a walk as symlink-TOCTOU prone.
		f, openErr := rootFS.Open(filepath.ToSlash(rel))
		if openErr != nil {
			return openErr
		}
		raw, readErr := io.ReadAll(f)
		_ = f.Close()
		if readErr != nil {
			return readErr
		}
		if bytes.Contains(raw, []byte("Code generated")) {
			return nil
		}
		m := scopeRE.FindSubmatch(raw)
		if m != nil && !want[string(m[1])] {
			t.Errorf("%s is SCOPE:%s but its owning unit declares %v — the registry is the source of truth for kinds",
				filepath.ToSlash(rel), m[1], classes)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", pkg, walkErr)
	}
}
