// SCOPE:feature — Theme toggle contract
//
// The suite renders the navbar from features/authviews and pins the
// single-toggle path (signal flip, icon sync, persistence order) plus the
// shared storage key across ThemeHead, the button expression, and the
// theme.js module — one owner, one button, one storage key. Rooted where
// the templates actually live: this package owns theme_head.templ and
// navbar.templ.
//
// It also pins the OTHER half of the contract, which is what a page can get
// wrong: the toggle is inside Navbar, so every page that renders Navbar must
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
	nb := authviews.Navbar("demo1@demo.app", "admin", "todo", "dev", "c0ffee", "daisyui")
	if err := nb.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render navbar: %v", err)
	}
	html := buf.String()
	start := strings.Index(html, "app-theme-toggle")
	if start < 0 {
		t.Fatal("navbar missing the theme toggle button")
	}
	end := strings.Index(html[start:], "</button>")
	if end < 0 {
		t.Fatal("navbar theme button never closes")
	}
	button := html[start : start+end]
	if !strings.Contains(button, "theme-toggle") {
		t.Error("theme button missing the .theme-toggle class")
	}
	// The single toggle path: signal flip (Datastar owns data-theme),
	// icon sync, persistence — in that order so a storage throw in
	// private mode cannot skip the visible change.
	for _, want := range []string{
		`data-on:click="`,
		`$theme = $theme === 'dark' ? 'light' : 'dark'`,
		`Theme.syncIcons($theme)`,
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
// actually caught a shipped bug. The toggle lives in Navbar, so rendering
// Navbar puts a working-looking button on the page. The button flips
// `$theme` and then calls Theme.syncIcons — but if <html> does not carry the
// bindings, nothing observes `$theme`: the icon sync hides BOTH icons (the
// CSS reveals the moon only under `html.dark`) and the theme never changes.
// The user sees the button vanish.
//
// Contract: a page that renders Navbar must either spread the shared
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
// red: `features/config/components/page.templ`, `features/room/components/page.templ`
// and `features/landing/components/index.templ` shipped with the Navbar and
// none of the three bindings.
func TestThemeTogglePagesWireTheSignal(t *testing.T) {
	const helper = "ThemeRootAttrs("

	var checked int
	var bad []string

	walkTemplSources(t, "../..", func(rel, text string) error {
		// Only pages that actually put the toggle on screen. The Navbar
		// component itself is skipped: it holds the button but no <html>,
		// and the assertion below is about the document element.
		if !strings.Contains(text, "<html") || !strings.Contains(text, "authviews.Navbar(") {
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
		t.Fatal("no page renders Navbar with <html> — the scan found nothing to check")
	}
	if len(bad) > 0 {
		t.Errorf("a page renders the theme toggle but does not wire $theme to "+
			"<html> (use authviews.ThemeRootAttrs, or carry the three bindings), "+
			"so clicking it hides both icons and changes nothing:\n  %s",
			strings.Join(bad, "\n  "))
	}
}

// TestIconifyIsLoadedOnceByTheHead pins the second half of the toggle bug,
// which was invisible in the markup: <iconify-icon> is a web component, so
// without /static/iconify-icon.min.js the tag lays out at 0x0 and the icon
// simply is not there. The navbar's theme and sound toggles are iconify icons,
// so a page missing the script shows a button that appears to vanish when the
// icon swaps — the reported "click it and it disappears".
//
// The fix moved the tag into ThemeHead, so:
//   - every PAGE that uses an iconify element must render ThemeHead (which
//     already was mandatory), and
//   - no page repeats the script itself (that is the duplication that let
//     three pages forget it).
//
// red: features/room/components/body.templ
// carried the navbar's iconify icons with no script tag at all, and eight
// other pages each carried their own copy.
func TestIconifyIsLoadedOnceByTheHead(t *testing.T) {
	const scriptTag = "iconify-icon.min.js"

	head, err := os.ReadFile("theme_head.templ")
	if err != nil {
		t.Fatalf("read theme_head: %v", err)
	}
	if !strings.Contains(string(head), scriptTag) {
		t.Errorf("ThemeHead does not load %s, so every iconify icon on every "+
			"page collapses to 0x0", scriptTag)
	}

	var usersWithoutHead, dupeCarriers []string
	walkTemplSources(t, "../..", func(rel, text string) error {
		if strings.Contains(rel, "theme_head") {
			return nil
		}
		// Only PAGES own a <head>. A component (like the sound toggle)
		// renders inside a page that already carries ThemeHead, so it is
		// not required to include one — and requiring it would be wrong,
		// since a component has no <head> to put it in.
		if !strings.Contains(text, "<html") {
			return nil
		}
		if strings.Contains(text, "<iconify-icon") && !strings.Contains(text, "ThemeHead(") {
			usersWithoutHead = append(usersWithoutHead, rel)
		}
		// No page may re-declare the script: one owner, the head.
		if strings.Contains(text, scriptTag) {
			dupeCarriers = append(dupeCarriers, rel)
		}
		return nil
	})

	if len(usersWithoutHead) > 0 {
		t.Errorf("these pages use <iconify-icon> but do not render ThemeHead, "+
			"which is what loads the component — without it the icon renders "+
			"at 0x0 and the toggle looks like it vanishes:\n  %s",
			strings.Join(usersWithoutHead, "\n  "))
	}
	if len(dupeCarriers) > 0 {
		t.Errorf("ThemeHead already loads %s; a page that repeats it is the "+
			"duplication that let others forget it:\n  %s", scriptTag,
			strings.Join(dupeCarriers, "\n  "))
	}
}
