// SCOPE:core - DO NOT REMOVE - markup reading for the class integrity gate.
//
// Everything that turns a .templ file into class tokens lives here: the
// attribute scanner, the Go-expression literal reader, the element-name
// lookback, and the DaisyUI part rules. Keeping it out of main.go also keeps
// each file under the repo's size cap.
package main

import (
	"fmt"
	"regexp"
	"strings"
)

// plainTokenRE is any class-shaped token: what a stylesheet selector can be.
var plainTokenRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// tokensInMarkup extracts the class tokens from `class=` attributes only:
// `class="a b"` and `class={ templ.Classes("a", map[string]bool{"is-on": c}) }`
// both yield their literals. `data-class` is skipped on purpose — those are
// state classes owned by Datastar expressions, not the static vocabulary.
func tokensInMarkup(src, rel string) []classToken {
	var out []classToken
	for _, span := range classAttrValues(src) {
		line := 1 + strings.Count(src[:span.start], "\n")
		for _, lit := range span.literals {
			for name := range strings.FieldsSeq(lit) {
				// EVERY class-shaped token: the "does any stylesheet define
				// this?" direction needs the utilities and the relics too, not
				// just this repo's app-*/ui-*/is- kit.
				if plainTokenRE.MatchString(name) {
					out = append(out, classToken{name: name, file: rel, line: line})
				}
			}
		}
	}
	return out
}

// valueSpan is one class attribute's value: where it starts, plus the string
// literals inside it.
type valueSpan struct {
	start    int
	literals []string
}

// classAttrValues finds `class=` followed by a quoted value or a `{ … }`
// expression, and returns the string literals it holds.
func classAttrValues(src string) []valueSpan {
	var out []valueSpan
	for i := 0; i+len("class=") < len(src); i++ {
		if !strings.HasPrefix(src[i:], "class=") {
			continue
		}
		// Reject `data-class=`, `foo-class=`: the byte before must not be part
		// of an identifier or a namespaced attribute.
		if i > 0 && (isIdentByte(src[i-1]) || src[i-1] == ':') {
			continue
		}
		j := i + len("class=")
		switch {
		case j < len(src) && src[j] == '"':
			end := strings.IndexByte(src[j+1:], '"')
			if end < 0 {
				continue
			}
			out = append(out, valueSpan{start: i, literals: []string{src[j+1 : j+1+end]}})
			i = j + end + 1
		case j < len(src) && src[j] == '{':
			end := exprEnd(src, j)
			out = append(out, valueSpan{start: i, literals: stringLiterals(src[j+1 : end])})
			i = end
		}
	}
	return out
}

// exprEnd returns the index of the `}` that closes the brace at open, skipping
// Go string literals (a `}` inside a literal is not a close). A brace-depth
// scan, not a regex, is what makes `templ.Classes("a", map[string]bool{…})`
// readable.
func exprEnd(src string, open int) int {
	depth := 0
	for k := open; k < len(src); k++ {
		switch src[k] {
		case '"':
			k = skipLiteral(src, k)
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return k
			}
		}
	}
	return len(src) - 1
}

// skipLiteral returns the index of the closing quote of the Go literal that
// starts at open, honouring a backslash escape.
func skipLiteral(src string, open int) int {
	for k := open + 1; k < len(src); k++ {
		if src[k] == '\\' {
			k++
			continue
		}
		if src[k] == '"' {
			return k
		}
	}
	return len(src) - 1
}

// stringLiterals returns the contents of the double-quoted literals in a Go
// expression, without escapes (there are none in class names).
func stringLiterals(expr string) []string {
	var out []string
	for i := 0; i < len(expr); i++ {
		if expr[i] != '"' {
			continue
		}
		j := i + 1
		var b strings.Builder
		for ; j < len(expr) && expr[j] != '"'; j++ {
			if expr[j] == '\\' && j+1 < len(expr) {
				j++
			}
			b.WriteByte(expr[j])
		}
		out = append(out, b.String())
		i = j
	}
	return out
}

func isIdentByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
}

// markupClass is one class token plus the element that carries it.
type markupClass struct {
	name string
	line int
	tag  string
}

// markupClasses reads every class attribute (expressions included) and pairs
// each token with its enclosing element.
func markupClasses(src string) []markupClass {
	var out []markupClass
	seen := map[string]bool{}
	for _, span := range classAttrValues(src) {
		line := 1 + strings.Count(src[:span.start], "\n")
		tag := enclosingTag(src, span.start)
		for _, lit := range span.literals {
			for name := range strings.FieldsSeq(lit) {
				key := fmt.Sprintf("%d|%s|%s", line, tag, name)
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, markupClass{name: name, line: line, tag: tag})
			}
		}
	}
	return out
}

// daisyFindings enforces the DaisyUI protocols using the vocabulary the VENDORED
// SKILL declares — no second copy of class names lives in this package.
//
// Three derived rules, and the reasoning for their boundaries:
//
//   - UNKNOWN: a class that starts with a component the file already uses must
//     be one the skill declares for it. `btn-primaryy` and `dropdown-contnt`
//     fail here. It only fires when the component base is present in the same
//     file, which is what keeps a Tailwind utility that shares a prefix with a
//     DaisyUI component (`mask-b-from-50`) out of it.
//   - PART: a part (`- part:` in the skill) needs its component in the same
//     file. This generalises `join-item` and `dropdown-content` to every part
//     to day one declared.
//   - HOST: DaisyUI classes are legitimately used on many elements, so host
//     policing is limited to FORM CONTROLS — the case where the class replaces
//     native semantics (`toggle` on a <button> renders but holds no state). The
//     component-to-host mapping comes from the skill's own syntax sample.
func daisyFindings(specs map[string]*componentSpec, src, rel string, repoOwned map[string]bool) []finding {
	var out []finding
	classes := markupClasses(src)

	present := map[string]bool{}
	for _, c := range classes {
		present[c.name] = true
	}
	for _, c := range classes {
		if repoOwned[c.name] || retiredClasses[c.name].when != "" {
			// A repo class that happens to share a DaisyUI prefix (`chat-layout`
			// next to DaisyUI's `chat`) is this repo's own vocabulary: chrome.css
			// wins over the library's namespace.
			continue
		}
		spec := owningComponent(specs, c.name)
		if spec == nil {
			continue
		}
		if !spec.Classes[c.name] && present[spec.Base] {
			out = append(out, finding{file: rel, line: c.line, msg: fmt.Sprintf(
				"daisyUI %q is not a %s class the vendored skill declares "+
					"(typo, or the skill is stale: scripts/refresh-daisyui.sh)", c.name, spec.Base)})
		}
		if spec.Parts[c.name] && !present[spec.Base] {
			out = append(out, finding{file: rel, line: c.line, msg: fmt.Sprintf(
				"daisyUI part %q needs its %q container in the same file", c.name, spec.Base)})
		}
		// Host policing needs ONE documented host that is a form control: the
		// case where the class replaces native state. A class the skill puts on
		// two elements (input on <input> and on a <label> container) is not
		// something markup can get wrong.
		if host, ok := singleFormHost(spec); ok && c.tag != "" && c.tag != host {
			out = append(out, finding{file: rel, line: c.line, msg: fmt.Sprintf(
				"daisyUI %q belongs on an <%s> (native state), found <%s>", spec.Base, host, c.tag)})
		}
	}
	return out
}

// owningComponent returns the component a class belongs to: the exact name
// first, then the longest base that is a `base-` prefix of the token.
func owningComponent(specs map[string]*componentSpec, name string) *componentSpec {
	if spec, ok := specs[name]; ok {
		return spec
	}
	var best *componentSpec
	for base, spec := range specs {
		if !strings.HasPrefix(name, base+"-") {
			continue
		}
		if best == nil || len(base) > len(best.Base) {
			best = spec
		}
	}
	return best
}

// enclosingTag walks back from a class attribute to the element that opened
// last: `<button\n class="…"` yields "button".
func enclosingTag(src string, pos int) string {
	for i := pos - 1; i >= 0; i-- {
		if src[i] != '<' {
			continue
		}
		j := i + 1
		if j < len(src) && src[j] == '/' {
			continue // a closing tag: keep walking
		}
		start := j
		for j < len(src) && (isIdentByte(src[j]) || src[j] == '-') {
			j++
		}
		if j > start {
			return strings.ToLower(src[start:j])
		}
	}
	return ""
}

// singleFormHost reports the one form element a component's class is documented
// on, when the skill agrees on exactly one.
func singleFormHost(spec *componentSpec) (string, bool) {
	if len(spec.Hosts) != 1 {
		return "", false
	}
	for host := range spec.Hosts {
		switch host {
		case "input", "select", "textarea":
			return host, true
		}
	}
	return "", false
}
