package capabilities

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Route-conformance: every /api/* URL a template or script fires must
// match a route registered in Go (renamed handler + stale template =
// silent dead button). Split from capabilities_test.go at the 500-line
// file budget; same package, no behavior change.

var apiRefRE = regexp.MustCompile(`['"](/api/[^'"?]*)/?[^'"]*['"]`)

// TestTemplAPIURLsHaveRoutes pins the .templ → Go route contract: every
// /api/* URL a template fires must match a route registered in Go.
// A renamed handler without its template is otherwise a silent dead
// button — Datastar gets a 404 and nothing renders.
func TestTemplAPIURLsHaveRoutes(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	routes := registeredAPIRoutes(t, root)
	refs := templAPIRefs(t, root)
	if len(refs) == 0 {
		t.Fatal("no /api/* references found in .templ — the scanner is broken, not the tree clean")
	}
	for _, ref := range refs {
		if !routeCovers(routes, ref) {
			t.Errorf("templ references %q with no registered route", ref)
		}
	}
}

// TestRouteCoversMatcher proves the negative: a bogus path must NOT
// match, or the tree scan above is green forever.
func TestRouteCoversMatcher(t *testing.T) {
	t.Parallel()
	routes := []string{"/api/todos", "/api/todos/{id}/delete", "/api/notes/fragment"}
	for _, tc := range []struct {
		ref  string
		want bool
	}{
		{"/api/todos", true},
		{"/api/todos/abc/delete", true},
		{"/api/notes/fragment", true},
		{"/api/todos/", true},
		{"/api/nope/missing", false},
		{"/api/todos/abc/bogus", false},
	} {
		if got := routeCovers(routes, tc.ref); got != tc.want {
			t.Errorf("routeCovers(%q) = %v, want %v", tc.ref, got, tc.want)
		}
	}
}

// registeredAPIRoutes collects every /api/* literal from non-test,
// non-generated Go sources. Reads through os.Root (see
// TestRegistryCoversDagnatsImports): a WalkDir callback acting on a
// bare path is a symlink TOCTOU (gosec G122).
func registeredAPIRoutes(t *testing.T, root string) []string {
	t.Helper()
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rootFS.Close() }()
	litRE := regexp.MustCompile(`"/api/[^"]*"`)
	seen := map[string]bool{}
	for _, tree := range []string{"features", "router", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, tree), func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			name := d.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") ||
				strings.HasSuffix(name, "_templ.go") {
				return nil
			}
			rel, relErr := filepath.Rel(root, p)
			if relErr != nil {
				return relErr
			}
			f, openErr := rootFS.Open(filepath.ToSlash(rel))
			if openErr != nil {
				return openErr
			}
			raw, readErr := io.ReadAll(f)
			_ = f.Close()
			if readErr != nil {
				return readErr
			}
			for _, m := range litRE.FindAllString(string(raw), -1) {
				seen[strings.Trim(m, `"`)] = true
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	out := make([]string, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	slices.Sort(out)
	return out
}

// templAPIRefs collects every static /api/* URL from .templ and .js
// sources: query stripped, concat prefixes kept (trailing /), prose
// placeholders (<id>, ..., spaces) skipped. .js literals are mostly
// concat prefixes ("/api/notes/" + id + "/op"); routeCovers matches
// those by prefix. Reads through os.Root like above.
func templAPIRefs(t *testing.T, root string) []string {
	t.Helper()
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rootFS.Close() }()
	seen := map[string]bool{}
	for _, tree := range []string{"features", "web"} {
		err := filepath.WalkDir(filepath.Join(root, tree), func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			name := d.Name()
			if !strings.HasSuffix(name, ".templ") && !strings.HasSuffix(name, ".js") {
				return nil
			}
			if strings.HasSuffix(name, "_templ.go") {
				return nil
			}
			rel, relErr := filepath.Rel(root, p)
			if relErr != nil {
				return relErr
			}
			f, openErr := rootFS.Open(filepath.ToSlash(rel))
			if openErr != nil {
				return openErr
			}
			raw, readErr := io.ReadAll(f)
			_ = f.Close()
			if readErr != nil {
				return readErr
			}
			for _, m := range apiRefRE.FindAllStringSubmatch(string(raw), -1) {
				ref := m[1]
				if idx := strings.IndexByte(ref, '?'); idx >= 0 {
					ref = ref[:idx]
				}
				if ref == "" || strings.ContainsAny(ref, "<> ") || strings.Contains(ref, "...") {
					continue
				}
				seen[ref] = true
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	out := make([]string, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	slices.Sort(out)
	return out
}

// routeCovers reports whether any registered route serves ref. A ref
// ending in / is a concat prefix (item.ID appended in markup): it
// covers when a route equals the prefix or extends it.
func routeCovers(routes []string, ref string) bool {
	if prefix, ok := strings.CutSuffix(ref, "/"); ok {
		for _, r := range routes {
			if r == prefix || strings.HasPrefix(r, prefix+"/") {
				return true
			}
		}
		return false
	}
	for _, r := range routes {
		if matchRoutePattern(r, ref) {
			return true
		}
	}
	return false
}

// matchRoutePattern matches a registered pattern (with {param}
// segments) against a concrete reference path.
func matchRoutePattern(pattern, ref string) bool {
	ps := strings.Split(pattern, "/")
	rs := strings.Split(ref, "/")
	if len(ps) != len(rs) {
		return false
	}
	for i := range ps {
		if strings.HasPrefix(ps[i], "{") {
			if rs[i] == "" {
				return false
			}
			continue
		}
		if ps[i] != rs[i] {
			return false
		}
	}
	return true
}
