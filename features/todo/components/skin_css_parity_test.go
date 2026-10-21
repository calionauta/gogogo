// SCOPE:feature — a CSS-only skin owes the shared markup a definition for every
// class name the markup spells.
//
// This is the regression guard for the `?skin=shadcn` breakage. The skin reuses
// the DaisyUI template tree and swaps only the stylesheet, so DaisyUI's
// component classes are the ONLY thing that styles the page — but the skin's
// bundle is built from its own `src/css/*-input.css`, which does not inherit
// them. Missing a definition is silent: the HTML renders, no console error
// fires, no Go test fails, and the control just collapses (`.join` sizing
// crushed the Add button; `.toast`, `.kbd`, `loading-spinner`, the checkbox
// colour variants and the `animate-in` / `slide-in-from-*` entrance utilities
// had no definition at all).
//
// `make css-check` cannot catch it: it proves the committed bundle matches its
// source, not that the source covers the markup. This test is the other half.
package components

import (
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/calionauta/gogogo/features/authviews"
	"github.com/calionauta/gogogo/web/resources"
	"github.com/calionauta/gogogo/web/skins"
)

// classAttrRE pulls class attribute values out of rendered HTML. Single quotes
// never appear on a class attribute in this tree (templ emits double quotes).
var classAttrRE = regexp.MustCompile(`class="([^"]*)"`)

// classTokenRE keeps real class tokens: no templ-conditional braces, no
// interpolation leftovers.
var classTokenRE = regexp.MustCompile(`^[a-zA-Z][\w-]*$`)

func renderClasses(t *testing.T, comps []templ.Component) []string {
	t.Helper()
	seen := map[string]bool{}
	var out []string
	for _, c := range comps {
		html := renderOrFail(t, c)
		for _, m := range classAttrRE.FindAllStringSubmatch(html, -1) {
			for tok := range strings.FieldsSeq(m[1]) {
				if classTokenRE.MatchString(tok) && !seen[tok] {
					seen[tok] = true
					out = append(out, tok)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// definedIn reports whether the stylesheet defines the class. Go's regexp has no
// lookahead, so the "not a longer class" check is done on the byte after the
// match by hand: `.card` must not be satisfied by `.card-body`.
func definedIn(css, class string) bool {
	needle := "." + class
	idx := 0
	for {
		i := strings.Index(css[idx:], needle)
		if i < 0 {
			return false
		}
		end := idx + i + len(needle)
		if end >= len(css) || !isClassByte(css[end]) {
			return true
		}
		idx = end
	}
}

// isClassByte reports whether b continues a CSS class name.
func isClassByte(b byte) bool {
	switch {
	case b == '-' || b == '_':
		return true
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	}
	return false
}

func readAsset(t *testing.T, name string) string {
	t.Helper()
	fsys := resources.StaticFS()
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		t.Fatalf("read embedded %s: %v", name, err)
	}
	return string(b)
}

// TestCSSOnlySkinCoversSharedMarkup walks the classes the SHARED template tree
// emits and requires each one to resolve in the skin's own bundle (plus the
// skin-independent `app.css`/`chrome.css`).
//
// Skins are listed by contract, not by name:
//   - an ASSETS-ONLY skin reuses the shared tree and must cover all of it;
//   - a skin with its OWN markup (Basecoat) is judged against its own tree —
//     its class vocabulary is attribute-driven (`[data-variant='primary']`
//     rather than `.btn-primary`), so the shared-tree sweep would report false
//     gaps for classes its templates never emit. It is covered by the browser
//     smoke pass instead.
func TestCSSOnlySkinCoversSharedMarkup(t *testing.T) {
	t.Parallel()
	signals := todoSignalsForRender()

	// The shared tree: exactly what every skin renders, from the four places
	// the shared vocabulary lives (todo page, navbar/login shell, skin picker).
	shared := []templ.Component{
		TodoList(signals),
		authviews.LoginPage("", "", "daisyui"),
		authviews.Navbar("user@example.com", "user", "todo", "test", "test0000", "daisyui"),
		authviews.ThemeHead(),
		skins.SkinSelector("daisyui"),
	}

	chrome := readAsset(t, "chrome.css")
	app := readAsset(t, "app.css")
	allBundles := chrome + app +
		readAsset(t, "app.min.css") + readAsset(t, "shadcn.min.css") + readAsset(t, "basecoat.min.css")

	for _, skin := range []string{"shadcn"} {
		skinCSS := chrome + app + readAsset(t, skin+".min.css")
		var gaps, hooks []string
		for _, class := range renderClasses(t, shared) {
			if definedIn(skinCSS, class) {
				continue
			}
			if !definedIn(allBundles, class) {
				// No bundle defines it anywhere: a styling-free hook class
				// (`todo-item` is a wiring handle, not a look).
				hooks = append(hooks, class)
				continue
			}
			gaps = append(gaps, class)
		}
		if len(gaps) > 0 {
			t.Errorf("skin %q does not style %d class(es) the shared markup emits: %s\n"+
				"       Add them to src/css/%s-input.css and run `make css-%s`.\n"+
				"       (hook classes with no definition anywhere are exempt: %s)",
				skin, len(gaps), strings.Join(gaps, " "), skin, skin, strings.Join(hooks, " "))
		}
	}
}

// TestCSSOnlySkinKeepsItsOwnBundle pins that the shadcn bundle is actually the
// one the skin serves — a rename that left the assets block pointing at
// app.min.css would make the parity test above meaningless.
func TestCSSOnlySkinKeepsItsOwnBundle(t *testing.T) {
	t.Parallel()
	html := renderOrFail(t, skins.SkinSelector("daisyui"))
	if !strings.Contains(html, "skin-menu-item") {
		t.Fatalf("skin selector lost its menu items; parity sweep would be vacuous")
	}
	// The asset block comes from the registry, so assert on the rendered
	// component rather than on a file name: a rename that left the skin
	// pointing at the DaisyUI bundle would make the sweep above vacuous.
	shadcnHTML := renderOrFail(t, skins.ActiveAssets("shadcn"))
	if !strings.Contains(shadcnHTML, "shadcn.min.css") {
		t.Fatalf("shadcn assets block does not load shadcn.min.css: %s", shadcnHTML)
	}
	if strings.Contains(shadcnHTML, "/static/app.min.css") {
		t.Fatalf("shadcn assets block loads app.min.css (the DaisyUI bundle): %s", shadcnHTML)
	}
}
