// SCOPE:core — Repo hygiene guard: removed surfaces may not come back.
//
// Two classes of dead weight are invisible to every other gate and were both
// present when the second UI skin was removed:
//
//   - Markup that references nothing. `data-variant` was on 28 elements and
//     no stylesheet had defined it since the skin that read it was deleted —
//     the attribute still parsed, still rendered, and did nothing.
//   - Prose that describes a removed surface. A doc naming a deleted skin sends
//     the reader looking for code that is not there, and a *contract* doc that
//     still promises it is worse than silence.
//
// Both are cheap to reintroduce by accident (copy a snippet, paste a paragraph),
// so they are pinned here rather than left to review attention.
//
// History is the exception, and the only one: CHANGELOG.md and the generated
// release pages record what shipped in past versions, and a decision record is
// what lets a reader see that the choice was made and later reversed. Rewriting
// those to match today would falsify the record.
package capabilities

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// removedBrands are names of surfaces this repo once shipped and no longer
// does. Adding one here is how a removal becomes permanent.
var removedBrands = []string{"basecoat", "web/skins", "skin_imports", "ui_skin", "?skin="}

// deadAttributes are attributes no stylesheet or script reads.
var deadAttributes = []string{"data-variant", "data-size"}

// historyFiles are allowed to mention removed surfaces: they are records of
// what shipped, owned by tools (release-please) or by the decision log.
func historyFiles(rel string) bool {
	switch {
	case rel == "internal/capabilities/no_legacy_guard_test.go",
		rel == "bin/red-proof-ui.sh":
		// The guard and its red-proof harness name the removed surface on
		// purpose: a ban has to spell out what it bans.
		return true
	case rel == "CHANGELOG.md":
		return true
	case strings.HasPrefix(rel, "site/releases/"):
		return true
	case rel == "docs/architecture-decision.md":
		return true
	case strings.HasPrefix(rel, "docs/research/"):
		// Research notes cite external component libraries as sources. Naming a
		// library that was studied is not a claim that this repo ships it.
		return true
	case strings.HasPrefix(rel, "site/docs/"),
		strings.HasPrefix(rel, "site/search/"),
		strings.HasPrefix(rel, "site/llms-"),
		rel == "site/sitemap.xml":
		// Generated from docs/ by `make site`. Scanning the sources is the
		// point; the rendered copies inherit their exemptions (and would
		// otherwise demand rewriting a citation into a lie).
		return true
	case strings.HasSuffix(rel, "package-lock.json"):
		// Lockfiles mirror registry metadata verbatim.
		return true
	}
	return false
}

// scannedExtensions are the files a reader or a compiler actually consumes.
var scannedExtensions = map[string]bool{
	".go": true, ".templ": true, ".css": true, ".js": true, ".mjs": true,
	".md": true, ".html": true, ".json": true, ".yml": true, ".yaml": true,
	".txt": true, ".sh": true,
}

// scanByName covers the build files that carry no extension. They are as
// load-bearing as any source file and were exactly where a stale reference
// survived the first sweep (Makefile, Dockerfile).
var scanByName = map[string]bool{
	"Makefile": true, "Dockerfile": true, "Dockerfile.dev": true,
	".golangci.yml": true, "lefthook.yml": true, "sgconfig.yml": true,
}

// TestNoRemovedBrandsInSourcesOrDocs fails when a removed surface is named
// anywhere a reader or build would consume it.
func TestNoRemovedBrandsInSourcesOrDocs(t *testing.T) {
	root := repoRoot(t)
	var violations []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", ".pi", "tmp", "data":
				return fs.SkipDir
			}
			return nil
		}
		scanned := scannedExtensions[strings.ToLower(filepath.Ext(rel))] || scanByName[d.Name()]
		if !scanned || historyFiles(rel) {
			return nil
		}
		// Minified bundles are build output; their strings come from the
		// sources this test already scans.
		if strings.HasSuffix(rel, ".min.css") || strings.HasSuffix(rel, ".min.js") {
			return nil
		}
		body, readErr := readFileString(path)
		if readErr != nil {
			return readErr
		}
		lower := strings.ToLower(body)
		for _, brand := range removedBrands {
			if strings.Contains(lower, brand) {
				violations = append(violations, rel+" mentions removed surface "+fmt.Sprintf("%q", brand))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if len(violations) > 0 {
		t.Fatalf("removed surfaces came back:\n  %s\n"+
			"(history belongs in CHANGELOG.md / site/releases/ / docs/architecture-decision.md)",
			strings.Join(violations, "\n  "))
	}
}

// TestNoDeadMarkupAttributes fails when markup carries an attribute that
// nothing styles or reads. Dead attributes look intentional, survive refactors,
// and mislead the next reader into thinking a skin/variant system exists.
func TestNoDeadMarkupAttributes(t *testing.T) {
	root := repoRoot(t)
	var violations []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", ".pi", "tmp", "data":
				return fs.SkipDir
			}
			return nil
		}
		// Generated templ repeats the sources verbatim; scanning the sources is
		// enough and keeps the failure message pointing at the file to edit.
		if strings.HasSuffix(path, "_templ.go") || !strings.HasSuffix(path, ".templ") {
			return nil
		}
		body, readErr := readFileString(path)
		if readErr != nil {
			return readErr
		}
		rel, _ := filepath.Rel(root, path)
		for _, attr := range deadAttributes {
			if strings.Contains(body, attr+"=") {
				violations = append(violations, rel+" carries "+attr)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if len(violations) > 0 {
		t.Fatalf("dead markup attributes (nothing reads them):\n  %s",
			strings.Join(violations, "\n  "))
	}
}

// readFileString reads a file as text, returning an error the caller ignores
// (an unreadable file is not a hygiene finding).
func readFileString(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}
