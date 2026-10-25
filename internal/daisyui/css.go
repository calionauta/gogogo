// SCOPE:core — stylesheet reading for the class gate.
//
// The rules need two different answers from CSS:
//
//   - which classes does the project's own stylesheet define? Those are the
//     project's vocabulary, and DaisyUI's namespace yields to them (a local
//     `chat-layout` is not a typo of DaisyUI's `chat`).
//   - which selectors does a stylesheet define at all? Used by the dead-CSS
//     direction in the repo gate.
//
// A class named in a COMMENT is not a definition: `StripCSSComments` runs first,
// because that mistake is exactly how a removed selector looks alive in review.
package daisyui

import (
	"regexp"
	"strings"
)

// selectorRE matches a plain class selector. Escaped or arbitrary selectors
// (`w-\\[3ch\\]`, `sm:p-2`) are not plain tokens and are skipped by callers
// that only care about names.
var selectorRE = regexp.MustCompile(`\.([A-Za-z][A-Za-z0-9_-]*)`)

// CSSClasses lists the class names a stylesheet defines.
func CSSClasses(raw string) map[string]bool {
	out := map[string]bool{}
	for _, m := range selectorRE.FindAllStringSubmatch(StripCSSComments(raw), -1) {
		out[m[1]] = true
	}
	return out
}

// StripCSSComments removes /* … */ so a comment that NAMES a class is not
// mistaken for a rule.
func StripCSSComments(css string) string {
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
