// SCOPE:core — the static-asset URL convention, checked at the SOURCE.
//
// The rendered sweep (features/todo/components, TestRenderedAssetsAreFingerprinted)
// proves the pages it renders emit fingerprinted URLs. It cannot see a template
// nothing renders, and every gap so far lived in exactly that blind spot: the
// feature bundles (notes.js, whiteboard.js, rough.min.js), the two helpers baked
// into shared templates (sse-open.js, session.js), and the scaffolded page in
// the installer's string constant. Each was a hardcoded "/static/..." path in a
// file no test rendered, so the rendered sweep kept passing while the browser
// kept revalidating every navigation.
//
// This walk reads the SOURCES instead: a hardcoded path is caught where it is
// written, including in templates and generated-page strings that no test
// renders. It is the mechanical half; the rendered sweep remains the proof that
// the helpers actually produce a fingerprinted URL at runtime.
package internal_test

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// bareStaticURLRE matches an asset reference with no query string. A
// fingerprinted URL carries ?v=<hash>, so the query-less form is the mistake:
// it revalidates on every navigation, and a CDN in front answers REVALIDATED
// instead of HIT.
var bareStaticURLRE = regexp.MustCompile(`(?:src|href)="(/static/[^"?]*)"`)

// scanRoots are the source trees that emit asset URLs, with the extensions to
// read. Only production sources: a test fixture that spells out the bug shape
// is asserting the check, not shipping the bug.
var scanRoots = []struct {
	dir  string
	exts []string
}{
	{".", []string{".templ"}},
	{"internal/installer", []string{".go"}},
}

// TestNoSourceHardcodesABareStaticURL fails when a template or the installer's
// page template spells out an asset path instead of fingerprinting it.
func TestNoSourceHardcodesABareStaticURL(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	var scanned int
	for _, scan := range scanRoots {
		base := filepath.Join(root, scan.dir)
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch d.Name() {
				case ".git", "node_modules", "site", "vendor", "testdata":
					return filepath.SkipDir
				}
				return nil
			}
			name := d.Name()
			if strings.HasSuffix(name, "_test.go") || !hasAnySuffix(name, scan.exts) {
				return nil
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			return scanAssetURLs(t, path, rel, &scanned)
		})
		if err != nil {
			t.Fatalf("walk %s: %v", scan.dir, err)
		}
	}
	if scanned == 0 {
		t.Fatal("no source file was scanned — the walk found nothing, so the rule would be vacuous")
	}
}

// scanAssetURLs reports each hardcoded reference in one file.
func scanAssetURLs(t *testing.T, path, rel string, scanned *int) error {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for line := 1; scanner.Scan(); line++ {
		text := scanner.Text()
		// A comment that shows the old pattern is documentation, not a page.
		if strings.HasPrefix(strings.TrimSpace(text), "//") {
			continue
		}
		(*scanned)++
		for _, m := range bareStaticURLRE.FindAllString(text, -1) {
			t.Errorf("%s:%d: %q hardcodes a static asset path: it revalidates on every "+
				"navigation (and a CDN answers REVALIDATED instead of HIT). Emit it through "+
				"resources.StaticURL, or the feature's own URL() helper.", rel, line, m)
		}
	}
	return scanner.Err()
}

// hasAnySuffix reports whether name ends in one of exts.
func hasAnySuffix(name string, exts []string) bool {
	for _, ext := range exts {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}
