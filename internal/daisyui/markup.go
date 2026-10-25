// SCOPE:core — markup reading for the class gate.
//
// Turns a .templ (or HTML) file into class tokens: the attribute scanner, the Go
// expression literal reader, and the element-name lookback. `Lint` builds on it;
// the repo gate uses the same extractor for its own directions, so "a class in a
// class attribute" means one thing everywhere.
package daisyui

import (
	"fmt"
	"regexp"
	"strings"
)

// Token is one class name plus where it was seen, so a failure can point at
// the line the reviewer has to open.
type Token struct {
	Name string
	File string
	Line int
}

// plainTokenRE is any class-shaped token: what a stylesheet selector can be.
var plainTokenRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// tokensInMarkup extracts the class tokens from `class=` attributes only:
// `class="a b"` and `class={ templ.Classes("a", map[string]bool{"is-on": c}) }`
// both yield their literals. `data-class` is skipped on purpose — those are
// state classes owned by Datastar expressions, not the static vocabulary.
func TokensInMarkup(src, rel string) []Token {
	var out []Token
	for _, span := range AttrValues(src) {
		line := 1 + strings.Count(src[:span.start], "\n")
		for _, lit := range span.literals {
			for name := range strings.FieldsSeq(lit) {
				// EVERY class-shaped token: the "does any stylesheet define
				// this?" direction needs the utilities and the relics too, not
				// just this repo's app-*/ui-*/is- kit.
				if plainTokenRE.MatchString(name) {
					out = append(out, Token{Name: name, File: rel, Line: line})
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
func AttrValues(src string) []valueSpan {
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

// enclosingTag walks back from a class attribute to the element that opened
// last: `<button\n class="…"` yields "button".
func EnclosingTag(src string, pos int) string {
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

// Class is one class token plus the element that carries it.
type Class struct {
	Name string
	Line int
	Tag  string
	Elem int // index of the class attribute this token came from
	Lit  int // index of the string literal inside that attribute
}

// ClassUses reads every class attribute (expressions included) and pairs each
// token with its enclosing element.
func ClassUses(src string) []Class {
	var out []Class
	seen := map[string]bool{}
	for elem, span := range AttrValues(src) {
		line := 1 + strings.Count(src[:span.start], "\n")
		tag := EnclosingTag(src, span.start)
		for litIndex, lit := range span.literals {
			for name := range strings.FieldsSeq(lit) {
				key := fmt.Sprintf("%d|%s|%d|%s", line, tag, litIndex, name)
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, Class{Name: name, Line: line, Tag: tag, Elem: elem, Lit: litIndex})
			}
		}
	}
	return out
}
