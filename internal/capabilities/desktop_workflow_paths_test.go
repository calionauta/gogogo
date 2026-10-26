// SCOPE:core — conformance guard for the Desktop workflow's path filter.
//
// WHY THIS EXISTS. `desktop.yml` is the only job that compiles cmd/desktop
// (scripts/web-packages.sh excludes it, so `make build`/`test`/`lint` and web
// CI never see it). It used to run on every pull request, which meant a
// JS/CSS/docs-only change paid for a GTK4/WebKitGTK install it could not
// break — and on one occasion paid thirty minutes for a hung apt step. It now
// runs only on the paths that can change the desktop binary.
//
// That narrowing has exactly one failure mode: `cmd/desktop/main.go` grows a
// direct import whose package is not in the workflow's list, and the job
// silently stops guarding it. This test is the observer.
//
// THE RULE: every module-level direct import of cmd/desktop/main.go must be
// covered by an entry in BOTH path lists (`pull_request` and `push`), and
// `cmd/desktop/**` must be there for the package itself.
//
// WHY DIRECT IMPORTS, not every transitive package: web CI compiles all of
// them — only cmd/desktop is excluded — so a transitive break is caught there.
// The desktop-only surface is main.go's CALL SITES, whose signatures live in
// its direct imports. A directory allowlist over `internal/**` + `features/**`
// would be "everything" in disguise and buy nothing.
//
// WHY PARSE INSTEAD OF GREP: the imports are Go syntax (`import "x"`, grouped
// or single, aliased or not), and the workflow is YAML that may express the
// lists as flow or block sequences. go/parser and yaml.v3 read the structures
// as written; a regex would pin the one shape already seen. yaml.v3 is already
// in the build graph (cmd/check-skill-frontmatter and the gh guard use it).
package capabilities

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const moduleImportPrefix = "github.com/calionauta/gogogo/"

// desktopWorkflowEvents are the two triggers that decide whether a change runs
// the desktop job. A list present on one and missing on the other is the
// half-filter that made stacked PRs skip the build.
var desktopWorkflowEvents = []string{"pull_request", "push"}

func TestDesktopWorkflowCoversDirectImports(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	mainPath := filepath.Join(root, "cmd", "desktop", "main.go")
	if _, err := os.Stat(mainPath); err != nil {
		if os.IsNotExist(err) {
			// The trim presets (minimal, no-ai) deliberately remove
			// cmd/desktop. There is no desktop binary left for the workflow
			// to guard in that tree, so this is not a failure.
			t.Skip("cmd/desktop/main.go is absent in this trimmed tree; the desktop workflow has nothing to cover")
		}
		t.Fatalf("stat %s: %v", mainPath, err)
	}

	imports := desktopDirectImports(t, mainPath)
	if len(imports) == 0 {
		t.Fatal("cmd/desktop/main.go has no module imports; the guard has nothing to check")
	}

	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "desktop.yml"))
	if err != nil {
		t.Fatalf("read desktop.yml: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse desktop.yml: %v", err)
	}
	on, ok := doc["on"].(map[string]any)
	if !ok {
		t.Fatal(`desktop.yml has no "on" mapping`)
	}

	for _, event := range desktopWorkflowEvents {
		trigger, ok := on[event].(map[string]any)
		if !ok {
			t.Errorf("desktop.yml: %q trigger is missing or not a mapping", event)
			continue
		}
		if _, hasIgnore := trigger["paths-ignore"]; hasIgnore {
			t.Errorf("desktop.yml: %q still carries paths-ignore; it wins over paths and the guard below would be moot", event)
		}
		paths := yamlStringList(trigger["paths"])
		if len(paths) == 0 {
			t.Errorf("desktop.yml: %q has no paths list", event)
			continue
		}
		if !desktopPathCovers(paths, "cmd/desktop") {
			t.Errorf("desktop.yml: %q paths do not cover cmd/desktop/**", event)
		}
		for _, dir := range imports {
			if desktopPathCovers(paths, dir) {
				continue
			}
			t.Errorf("desktop.yml: %q paths do not cover %q, a direct import of cmd/desktop/main.go — add %q to both lists",
				event, dir, dir+"/**")
		}
	}
}

// desktopDirectImports returns the module-relative directories of main.go's
// direct imports ("internal/server", "config"), sorted for stable failure text.
func desktopDirectImports(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var dirs []string
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(importPath, moduleImportPrefix) {
			continue
		}
		dirs = append(dirs, strings.TrimPrefix(importPath, moduleImportPrefix))
	}
	sort.Strings(dirs)
	return dirs
}

// desktopPathCovers reports whether a path-filter entry covers a package dir.
// Only the two shapes the workflow actually uses count: the directory itself
// and `<dir>/**` for everything under it. A broader glob (`**/*.go`) is
// deliberately NOT accepted: it would have to be understood and kept in sync
// in the author's head, while `<dir>/**` names the package the guard checks.
func desktopPathCovers(paths []string, dir string) bool {
	for _, entry := range paths {
		if entry == dir || entry == dir+"/**" {
			return true
		}
	}
	return false
}

// yamlStringList reads a YAML sequence of strings, tolerating a nil value.
func yamlStringList(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
