// SCOPE:feature — Theme toggle contract
//
// The suite renders the shell from features/authviews and pins the
// single-toggle path (signal flip, icon sync, persistence order) plus the
// shared storage key across ThemeHead, the button expression, and the
// theme.js module — one owner, one button, one storage key. Rooted where
// the templates actually live: this package owns theme_head.templ and
// shell.templ.
//
// It also pins the OTHER half of the contract, which is what a page can get
// wrong: the toggle is inside Shell, so every page that renders Shell must
// ALSO wire the `$theme` signal to <html>. Miss the wiring and the button
// still renders and still runs, but nothing observes the signal — the click
// hides both icons and the theme never changes (the exact failure the
// theme.js header warns about). Three pages shipped that bug; this test is
// what makes the next one loud instead of visual.
package authviews_test

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/authviews"
)

// walkTemplSources visits every .templ source under root, handing each
// callback its repo-relative slash path and its text. Both tests below need
// the same traversal, and both need it to read through os.Root: a WalkDir
// callback acting on a bare path is a symlink TOCTOU (gosec G122). The
// repo's other template scans do the same (see
// internal/capabilities/capabilities_routes_test.go).
func walkTemplSources(t *testing.T, root string, fn func(rel, text string) error) {
	t.Helper()
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rootFS.Close() }()

	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Vendored/build trees can never be a page.
			switch d.Name() {
			case "node_modules", ".git", "vendor":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".templ") {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		f, openErr := rootFS.Open(rel)
		if openErr != nil {
			return openErr
		}
		raw, readErr := io.ReadAll(f)
		closeErr := f.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		return fn(rel, string(raw))
	})
	if err != nil {
		t.Fatalf("walk templates: %v", err)
	}
}

// TestThemeToggleSingleOwner pins the shared theme-toggle contract.
func TestThemeToggleSingleOwner(t *testing.T) {
	var buf bytes.Buffer
	if err := authviews.ThemeToggle().Render(context.Background(), &buf); err != nil {
		t.Fatalf("render theme toggle: %v", err)
	}
	html := buf.String()
	start := strings.Index(html, "app-theme-toggle")
	if start < 0 {
		t.Fatal("theme toggle button missing")
	}
	end := strings.Index(html[start:], "</button>")
	if end < 0 {
		t.Fatal("theme button never closes")
	}
	button := html[start : start+end]
	if !strings.Contains(button, "theme-toggle") {
		t.Error("theme button missing the .theme-toggle class")
	}
	// The single toggle path: signal flip (Datastar owns data-theme) then
	// persistence — in that order so a storage throw in private mode cannot
	// skip the visible change. The icons are inline SVG whose visibility the
	// [data-theme] CSS rules own, so no script call belongs here.
	for _, want := range []string{
		`data-on:click="`,
		`$theme = $theme === 'dark' ? 'light' : 'dark'`,
		`localStorage.setItem('themeMode', $theme)`,
	} {
		if !strings.Contains(button, want) {
			t.Errorf("theme button missing %q (the single toggle path)", want)
		}
	}

	// One storage key across the pre-paint bootstrap and the button.
	head, err := os.ReadFile("theme_head.templ")
	if err != nil {
		t.Fatalf("read theme_head: %v", err)
	}
	if !strings.Contains(string(head), "themeMode") {
		t.Error("ThemeHead left the shared storage key")
	}
	if !strings.Contains(button, "themeMode") {
		t.Error("button expression left the shared storage key")
	}
	js, err := os.ReadFile("../../web/resources/static/theme.js")
	if err != nil {
		t.Fatalf("read theme.js: %v", err)
	}
	if strings.Contains(string(js), `setAttribute("data-theme"`) {
		t.Error("theme.js must not write data-theme (Datastar reverts " +
			"external writes to bound attributes; the $theme signal owns it)")
	}
}

// TestThemeTogglePagesWireTheSignal is the other half, and the one that
// actually caught a shipped bug. The toggle lives in Shell, so rendering
// Shell puts a working-looking button on the page. The button flips
// `$theme` — but if <html> does not carry the bindings, nothing observes
// `$theme`: the icon CSS reveals the moon only under `html.dark`, so the
// click hides BOTH icons and the theme never changes. The user sees the
// button vanish.
//
// Contract: a page that renders Shell must either spread the shared
// authviews.ThemeRootAttrs helper (the canonical form — see
// theme_root.go) or carry the three bindings itself:
//
//	data-signals                (seeds $theme; also read by ThemeHead)
//	data-attr:data-theme        ($theme drives the attribute)
//	data-class:dark             ($theme drives the class the icon CSS keys on)
//
// The helper is preferred because this exact copy-on-every-page is what
// diverged: three pages forgot the attributes entirely. Accepting both forms
// keeps the scan honest about intent ("is $theme wired?") rather than
// prescribing one spelling.
//
// Scanning templates rather than rendered output is deliberate: the defect is
// in the template's <html> element, and a render-based test would need to
// drive every page's handler.
//
// The set of templates that own a `<html>` element is now small on purpose:
// `internal/ui/shell.templ` (every page, landing included) plus the one Decide
// surface that stays outside the shell (login). Each one must spread the shared
// helper; the failure message names the file so a new page cannot ship with a
// toggle that renders and does nothing.
//
// red: `features/config/components/page.templ` and
// `features/room/components/page.templ` once shipped with the toggle wired by
// hand and one binding missing.
func TestThemeTogglePagesWireTheSignal(t *testing.T) {
	const helper = "ThemeRootAttrs("

	var checked int
	var bad []string

	walkTemplSources(t, "../..", func(rel, text string) error {
		// Only templates that own the document element. A component that holds
		// the button but no <html> is skipped: the assertion is about the
		// element the bindings have to sit on.
		// Match the ELEMENT, not the word: this package's own comments and an
		// inline script mention `<html>` while owning no document.
		if !strings.Contains(text, "<html lang") {
			return nil
		}
		checked++

		// The canonical form: the shared helper, which carries all three.
		if strings.Contains(text, helper) {
			return nil
		}

		var missing []string
		for _, want := range []string{
			"data-signals",
			"data-attr:data-theme",
			"data-class:dark",
		} {
			// All three live on the same <html ...> line; scan the whole
			// template so a multi-line element still matches.
			if !strings.Contains(text, want) {
				missing = append(missing, want)
			}
		}
		if len(missing) > 0 {
			bad = append(bad, rel+" missing "+strings.Join(missing, ", "))
		}
		return nil
	})

	if checked == 0 {
		t.Fatal("no template owns <html> — the scan found nothing to check")
	}
	if len(bad) > 0 {
		t.Errorf("<html> owner that does not wire $theme to "+
			"<html> (use authviews.ThemeRootAttrs, or carry the three bindings), "+
			"so clicking it hides both icons and changes nothing:\n  %s",
			strings.Join(bad, "\n  "))
	}
}

// TestNoIconifyRemains pins the post-iconify contract: every glyph is inline
// SVG from internal/icon, so no template may reference the web component or
// its script, and the asset itself must be gone from web/resources/static.
// Unlike the old wiring test this scans EVERY .templ — the script's second
// owner, internal/components/assets.templ, owns no <html> and slipped through
// the old page-only scan, which is how the double-load shipped.
//
// red: re-adding an iconify tag or the script src anywhere in a .templ,
// or restoring the static asset.
func TestNoIconifyRemains(t *testing.T) {
	var bad []string
	walkTemplSources(t, "../..", func(rel, text string) error {
		for _, needle := range []string{"<iconify-icon", "iconify-icon.min.js"} {
			if strings.Contains(text, needle) {
				bad = append(bad, rel+" contains "+needle)
			}
		}
		return nil
	})

	if len(bad) > 0 {
		t.Errorf("iconify is gone (inline SVG in internal/icon) — "+
			"these templates still reference it:\n  %s",
			strings.Join(bad, "\n  "))
	}
	if _, err := os.Stat("../../web/resources/static/iconify-icon.min.js"); !os.IsNotExist(err) {
		t.Error("web/resources/static/iconify-icon.min.js still exists — delete the asset")
	}
}
