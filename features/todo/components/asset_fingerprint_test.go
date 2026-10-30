// SCOPE:core — the cache contract for every embedded asset.
//
// The contract, in one place: an asset served to a browser must be either
// fingerprinted (immutable, cached for a year) or explicitly bare. The bare
// case still works — it revalidates against its content-hash ETag — but it
// costs a round trip per asset per navigation, and a CDN in front of the app
// answers REVALIDATED instead of HIT. With the origin reachable only through
// a tunnel, that is the difference between an edge hit and crossing the
// tunnel for a 348 KB stylesheet.
//
// Why a test: the policy was written in a comment and never enforced, so the
// first fingerprinting pass covered the theme bundle and left the feature
// bundles, the two helper scripts baked into templates, and the scaffolded
// pages on the revalidating path — with nothing to notice. The guard below
// walks the RENDERED HTML (not the sources), so a template that hardcodes a
// path is caught wherever it lives.
package components

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/authviews"
	"github.com/calionauta/gogogo/features/todo"
	"github.com/calionauta/gogogo/internal/components"
	"github.com/calionauta/gogogo/internal/ui"
	"github.com/calionauta/gogogo/web/resources"
)

// bareAssetRE matches a static asset reference without a fingerprint.
var bareAssetRE = regexp.MustCompile(`(?:src|href)="(/static/[^"?]*[^"?])"`)

// renderAll collects the HTML of every page shell that loads static assets.
func renderAll(t *testing.T) string {
	t.Helper()
	signals := todo.Signals{
		Filter:         "all",
		LLMEnabled:     true,
		SimulatedLLM:   true,
		DagNatsEnabled: true,
	}
	parts := []string{
		renderOrFail(t, TodoList(signals)),
		renderOrFail(t, authviews.LoginPage("", "", true, "", "")),
		renderOrFail(t, authviews.UserMenu("u@example.com", "user", nil, false, false)),
		renderOrFail(t, authviews.ThemeHead()),
		renderOrFail(t, ui.Preferences()),
		renderOrFail(t, components.Assets()),
		renderOrFail(t, components.SessionBanner(true)),
		renderOrFail(t, components.StreamOpener("tab-1", "/api/todos/stream", false)),
	}
	return strings.Join(parts, "\n")
}

// TestRenderedAssetsAreFingerprinted is the guard. Every static reference in
// the rendered pages must carry its ?v= fingerprint.
func TestRenderedAssetsAreFingerprinted(t *testing.T) {
	t.Parallel()
	html := renderAll(t)

	matches := bareAssetRE.FindAllString(html, -1)
	if len(matches) == 0 {
		return // nothing to assert: the pages load no static asset
	}
	for _, m := range matches {
		t.Errorf("static asset %q is referenced WITHOUT a ?v= fingerprint: it revalidates "+
			"on every navigation (and a CDN answers REVALIDATED instead of HIT). "+
			"Emit it through resources.StaticURL / the feature's URL() helper.", m)
	}
}

// TestEveryEmbeddedAssetHasAFingerprint proves the other half: the helper can
// name EVERY file in the core bundle. An asset that the helper cannot hash is
// an asset no template can fingerprint, which is how the gap started.
func TestEveryEmbeddedAssetHasAFingerprint(t *testing.T) {
	t.Parallel()
	fsys := resources.StaticFS()

	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		url := resources.StaticURL(p)
		if !strings.Contains(url, "?v=") {
			t.Errorf("StaticURL(%q) = %q, want a fingerprinted URL", p, url)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk static fs: %v", err)
	}
}
