// SCOPE:core — Service worker stream-skip contract.
package server

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/web/resources"
)

// sseRouteRE finds the `/api/*` literals a feature registers as a stream.
// Trimmed trees are the reason this is derived, not a fixed list: a tree
// without chat has no `/api/chat/stream` route, and the installer removes the
// corresponding SW line — the test must follow the tree, not the full template.
var sseRouteRE = regexp.MustCompile(`"(/api/[^"]*(?:stream|events)[^"]*)"`)

// registeredSSERoutes walks the shipped Go sources for stream routes. The
// installer is excluded on purpose: its trim data names routes as strings, not
// as served handlers (the same exclusion the API inventory makes), and tests
// and generated templates are not route registrations.
func registeredSSERoutes(t *testing.T, root string) map[string]bool {
	t.Helper()
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("open root: %v", err)
	}
	defer func() { _ = rootFS.Close() }()

	routes := map[string]bool{}
	for _, tree := range []string{"features", "internal"} {
		walkErr := filepath.WalkDir(filepath.Join(root, tree), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				if rel == "internal/installer" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") ||
				strings.HasSuffix(path, "_test.go") ||
				strings.HasSuffix(path, "_templ.go") {
				return nil
			}
			f, openErr := rootFS.Open(rel)
			if openErr != nil {
				return openErr
			}
			raw, readErr := io.ReadAll(f)
			_ = f.Close()
			if readErr != nil {
				return readErr
			}
			for _, m := range sseRouteRE.FindAllStringSubmatch(string(raw), -1) {
				routes[m[1]] = true
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", tree, walkErr)
		}
	}
	return routes
}

// TestServiceWorkerSkipsEverySSERoute pins the gotcha that has bitten the
// session stream: the service worker intercepts /api/*, and caching an SSE
// response never completes. The response clone keeps the stream open, the
// browser's EventSource sees an error, reconnects, and each retry adds a
// connection — after a handful of navigations the six-per-host budget is gone
// and the next page hangs on load.
//
// The check follows the tree: every stream route the sources still register
// must be skipped. A path with a route parameter is covered by its prefix
// (`/api/notes/{docID}/stream` → `/api/notes/`).
func TestServiceWorkerSkipsEverySSERoute(t *testing.T) {
	raw, err := fs.ReadFile(resources.StaticFS(), "sw.js")
	if err != nil {
		t.Fatalf("read sw.js: %v", err)
	}
	sw := string(raw)
	routes := registeredSSERoutes(t, filepath.Join("..", ".."))
	if len(routes) == 0 {
		t.Fatal("no /api stream route found — the scanner is broken, not the tree clean")
	}
	for route := range routes {
		prefix := route
		if i := strings.Index(prefix, "{"); i >= 0 {
			prefix = prefix[:i]
		}
		if !strings.Contains(sw, `"`+prefix+`"`) {
			t.Errorf("sw.js SKIP_PATTERNS missing %q (route %q) — the service worker will cache an SSE "+
				"response, the EventSource will retry, and every page after that can starve "+
				"on the browser's per-host connection cap", prefix, route)
		}
	}
}
