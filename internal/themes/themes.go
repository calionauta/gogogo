// SCOPE:core — The shipped theme catalog: the one list the UI, the pre-paint
// bootstrap and theme.js all read.
//
// Themes are DaisyUI palettes compiled into app.min.css (src/css/input.css).
// Three places need to know them, and before this package each kept its own
// copy: the selector (Go), the pre-paint bootstrap (inline JS in
// features/authviews/theme_head.templ) and the runtime controller
// (web/resources/static/theme.js). Adding a theme meant editing three lists in
// two languages, and a miss showed up as "the app's dark chrome stays light".
//
// Now the list lives here; the shell renders it as the picker, and the two JS
// sites read `data-theme-dark` (emitted by authviews.ThemeRootAttrs) instead of
// hardcoding names. themes_test.go keeps this list and input.css in step.
package themes

import "strings"

// Theme is one selectable palette.
type Theme struct {
	Name  string // the data-theme value (DaisyUI theme name)
	Label string // human label in the picker
	Dark  bool   // drives the html.dark chrome rules and the picker's icon
}

// All is the catalog, in picker order. Keep in step with the `themes:` list in
// src/css/input.css — themes_test.go enforces it.
var All = []Theme{
	{Name: "light", Label: "Light"},
	{Name: "dark", Label: "Dark", Dark: true},
	{Name: "lofi", Label: "Calm"},
	{Name: "dim", Label: "Zinc", Dark: true},
}

// Is reports whether name is a shipped theme.
func Is(name string) bool {
	for _, t := range All {
		if t.Name == name {
			return true
		}
	}
	return false
}

// IsDark reports whether the named theme is a dark palette. Unknown names fall
// back to the document's own color-scheme in the browser, so this answers only
// for shipped themes.
func IsDark(name string) bool {
	for _, t := range All {
		if t.Name == name {
			return t.Dark
		}
	}
	return false
}

// DarkNames is the space-separated list the browser reads from
// data-theme-dark: one source for "which palettes are dark".
func DarkNames() string {
	var out []string
	for _, t := range All {
		if t.Dark {
			out = append(out, t.Name)
		}
	}
	return strings.Join(out, " ")
}
