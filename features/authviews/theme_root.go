// SCOPE:feature — Theme toggle contract
//
// This file owns the ONE place the `$theme` signal is wired to the document
// element. Every page used to copy the same three attributes onto its own
// <html>, and three of them eventually forgot — a page that renders the
// toggle from Navbar but does not wire the signal shows a button whose click
// hides both icons and changes nothing. The ThemeRootAttrs helper makes the
// wiring a call instead of a copy, so "does this page theme correctly?" is
// answered by looking for one line, not by diffing eleven <html> tags.
//
// ThemeHead (theme_head.templ) is the other half and stays separate on
// purpose: it must be the FIRST child of <head> to run before first paint.
// The signals attribute here is what it reads and merges into.
package authviews

import "github.com/a-h/templ"

// themeSignalSeed is the JSON the document element is seeded with. It carries
// `theme` because the signal must exist before Datastar reads the attribute,
// and ThemeHead merges the persisted/OS-resolved value into this same object
// (replacing the whole attribute would drop every other seed on the page).
const themeSignalSeed = `{"theme":"light"}`

// ThemeRootAttrs returns the attributes a page's <html> element must carry for
// the theme toggle to work:
//
//	data-signals          — seeds $theme, and is what ThemeHead merges into
//	data-attr:data-theme  — $theme drives the document's data-theme
//	data-class:dark       — $theme drives the `dark` class the icon CSS keys on
//
// Use it as a spread, which keeps the caller's own signals:
//
//	<html lang="en" { authviews.ThemeRootAttrs(self.Signals)... }>
//
// `signals` is the caller's own seeded JSON (its page-specific state); pass
// "" for a page that has none. The returned map is fresh on every call so a
// caller may mutate it before spreading.
//
// The third attribute is the one that is easy to think is redundant: the icon
// CSS reveals the moon only under `html.dark`, so without the class the base
// rule hides it and the inline `display:""` that syncIcons writes does not win
// — both icons disappear.
func ThemeRootAttrs(signals string) templ.Attributes {
	if signals == "" {
		signals = themeSignalSeed
	}
	return templ.Attributes{
		"data-signals":         signals,
		"data-attr:data-theme": `$theme`,
		"data-class:dark":      `$theme === 'dark'`,
	}
}
