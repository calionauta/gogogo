// SCOPE:core - DO NOT REMOVE - stylesheet reading for the class integrity gate.
//
// `definedClasses` answers "does the repo's own kit define this class", and
// `stripCSSComments` makes sure a class NAMED in a comment is not mistaken for
// a rule — that mistake is how a dead selector could look alive in review.
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// definedClasses collects `.app-*` / `.ui-*` / `.is-*` selectors, ignoring CSS
// comments so prose about a class cannot pass as a definition.
func definedClasses(css string) map[string]bool {
	out := map[string]bool{}
	for _, m := range definedRE.FindAllStringSubmatch(stripCSSComments(css), -1) {
		out[m[1]] = true
	}
	return out
}

// stripCSSComments removes /* … */ so a comment that NAMES a class is not
// mistaken for a rule.
func stripCSSComments(css string) string {
	var b strings.Builder
	inComment := false
	for line := range strings.SplitSeq(css, "\n") {
		for {
			if inComment {
				idx := strings.Index(line, "*/")
				if idx < 0 {
					line = ""
					break
				}
				line = line[idx+2:]
				inComment = false
				continue
			}
			idx := strings.Index(line, "/*")
			if idx < 0 {
				break
			}
			rest := line[idx+2:]
			if _, after, found := strings.Cut(rest, "*/"); found {
				line = line[:idx] + after
				continue
			}
			inComment = true
			line = line[:idx]
			break
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// handwrittenStylesheets are the stylesheets a human edits. The BUILT bundle
// (app.min.css) is deliberately NOT among them: it contains whatever DaisyUI and
// Tailwind emitted, so treating it as "the repo owns this class" would let a
// stale-but-present class skip the skill check — which is exactly how a
// vocabulary drifts. `chat-layout` (chrome.css) counts as ours; `toggle-sm` must
// still be declared by the skill.
var handwrittenStylesheets = []string{
	"web/resources/static/app.css",
	"web/resources/static/chrome.css",
	"site/styles.css",
}

// selectorRE matches a plain class selector. Escaped or arbitrary selectors
// (`w-\[3ch\]`, `sm:p-2`) are not plain tokens and are skipped by the caller.
var selectorRE = regexp.MustCompile(`\.([A-Za-z][A-Za-z0-9_-]*)`)

// loadStylesheets returns the class names every shipped stylesheet defines.
func loadHandwritten(root string) (map[string]bool, error) {
	out := map[string]bool{}
	paths, err := stylesheetFiles(root)
	if err != nil {
		return nil, err
	}
	for _, rel := range paths {
		// #nosec G703 -- local dev tool; the paths come from a glob under root.
		raw, rerr := os.ReadFile(filepath.Join(root, rel))
		if rerr != nil {
			return nil, rerr
		}
		for _, m := range selectorRE.FindAllStringSubmatch(stripCSSComments(string(raw)), -1) {
			out[m[1]] = true
		}
	}
	return out, nil
}

func stylesheetFiles(root string) ([]string, error) {
	var out []string
	for _, rel := range handwrittenStylesheets {
		matches, err := filepath.Glob(filepath.Join(root, rel))
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			rel, rerr := filepath.Rel(root, m)
			if rerr != nil {
				return nil, rerr
			}
			out = append(out, filepath.ToSlash(rel))
		}
	}
	sort.Strings(out)
	return out, nil
}
