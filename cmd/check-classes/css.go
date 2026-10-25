// SCOPE:core - DO NOT REMOVE - stylesheet reading for the repo class gate.
//
// Two questions, both answered by internal/daisyui now:
//
//   - which selectors does the repo's own kit define? (`chrome.css` — the
//     `app-*`/`ui-*`/`is-*` vocabulary the repo owns)
//   - which classes do the HAND-WRITTEN stylesheets define? (the set DaisyUI's
//     namespace yields to, so `chat-layout` is never judged against `chat`)
//
// The BUILT bundle (`app.min.css`) is deliberately not among the hand-written
// ones: it contains whatever DaisyUI and Tailwind emitted, so treating it as
// "the repo owns this class" would let a stale-but-present class skip the skill
// check — the exact drift the gate exists to catch.
package main

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/calionauta/gogogo/internal/daisyui"
)

// handwrittenStylesheets are the stylesheets a human edits.
var handwrittenStylesheets = []string{
	"web/resources/static/app.css",
	"web/resources/static/chrome.css",
	"site/styles.css",
}

// loadDefined returns the repo kit's own selectors (chrome.css).
func loadDefined(root string) (map[string]bool, error) {
	// #nosec G703 -- local dev tool; root comes from the developer's own shell.
	raw, err := os.ReadFile(filepath.Join(root, chromeCSS))
	if err != nil {
		return nil, err
	}
	return definedClasses(string(raw)), nil
}

// definedClasses keeps only the classes this repo's kit owns; a stylesheet
// defines plenty of utilities too, and those are not its vocabulary.
func definedClasses(css string) map[string]bool {
	out := map[string]bool{}
	for class := range daisyui.CSSClasses(css) {
		if isLocalKit(class) {
			out[class] = true
		}
	}
	return out
}

// loadHandwritten returns every class any hand-written stylesheet defines.
func loadHandwritten(root string) (map[string]bool, error) {
	out := map[string]bool{}
	paths, err := stylesheetFiles(root)
	if err != nil {
		return nil, err
	}
	for _, rel := range paths {
		// #nosec G703 -- local dev tool; the paths come from a fixed list under root.
		raw, rerr := os.ReadFile(filepath.Join(root, rel))
		if rerr != nil {
			return nil, rerr
		}
		for class := range daisyui.CSSClasses(string(raw)) {
			out[class] = true
		}
	}
	return out, nil
}

// stylesheetFiles lists the hand-written stylesheets that exist in this tree.
func stylesheetFiles(root string) ([]string, error) {
	var out []string
	for _, rel := range handwrittenStylesheets {
		matches, err := filepath.Glob(filepath.Join(root, rel))
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			r, rerr := filepath.Rel(root, m)
			if rerr != nil {
				return nil, rerr
			}
			out = append(out, filepath.ToSlash(r))
		}
	}
	sort.Strings(out)
	return out, nil
}
