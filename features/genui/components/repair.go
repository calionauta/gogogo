// SCOPE:plugin — the tolerant front door of the component catalog.
package components

import "strings"

// repairJSON normalizes the two malformations small models produce most
// often, and leaves valid JSON byte-identical. Labels go first: dropping
// a wrapper can expose a comma the second pass then has to strip.
//
// Every scan respects string literals (a comma or colon inside "a, b"
// never triggers), so strictness is otherwise unchanged — what does not
// parse is still the strict decoder's call.
func repairJSON(s string) string {
	return stripTrailingCommas(dropRedundantLabels(s))
}

// stripTrailingCommas drops the comma in ",}" and ",]": the most common
// small-model malformation.
func stripTrailingCommas(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inStr, esc := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
			b.WriteByte(c)
		case c == '\\' && inStr:
			esc = true
			b.WriteByte(c)
		case c == '"':
			inStr = !inStr
			b.WriteByte(c)
		case !inStr && c == ',':
			if followedByCloser(s, i) {
				continue
			}
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// frame is one open bracket, so the scan below knows whether a string
// literal sits in element position (inside an array) and which brackets
// belong to a wrapper it dropped.
type frame struct {
	ch   byte // '[' or '{'
	drop bool // redundant wrapper: emit neither bracket
}

// dropRedundantLabels removes a repeated envelope key used where an
// array ELEMENT belongs. The model writes the wrapper twice, `{"components":
// [{…},"components":[{…}]]}`, which is not JSON at all (Go: "invalid
// character ':' after array element") even though the intent — two
// sibling components — is unambiguous. The label and its colon go; when
// the value is an array its brackets go too, so its elements splice into
// the array they were meant to extend.
func dropRedundantLabels(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	var stack []frame
	inStr, esc := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
			b.WriteByte(c)
		case c == '\\' && inStr:
			esc = true
			b.WriteByte(c)
		case c == '"':
			if !inStr {
				if resume, wrap, ok := redundantKey(s, i, stack); ok {
					stack = pushWrapper(stack, wrap)
					i = resume
					continue
				}
			}
			inStr = !inStr
			b.WriteByte(c)
		case inStr:
			b.WriteByte(c)
		case c == '[' || c == '{':
			stack = append(stack, frame{ch: c})
			b.WriteByte(c)
		case c == ']' || c == '}':
			if f, ok := pop(&stack); ok && f.drop {
				continue
			}
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// pushWrapper records that the just-dropped label also owned the value's
// brackets, so neither bracket is emitted (its elements splice in).
func pushWrapper(stack []frame, wrap bool) []frame {
	if !wrap {
		return stack
	}
	return append(stack, frame{ch: '[', drop: true})
}

// pop removes the innermost open bracket.
func pop(stack *[]frame) (frame, bool) {
	if len(*stack) == 0 {
		return frame{}, false
	}
	f := (*stack)[len(*stack)-1]
	*stack = (*stack)[:len(*stack)-1]
	return f, true
}

// redundantKey reports whether the string literal starting at i is a
// repeated wrapper label sitting in array-element position, and where
// the scan resumes once the label (and, when wrap, the value's own
// brackets) has been dropped. A string in object position is a real key
// and never matches; the same is true of every string element of a valid
// array, because none is followed by a colon.
func redundantKey(s string, i int, stack []frame) (resume int, wrap, ok bool) {
	if len(stack) == 0 || stack[len(stack)-1].ch != '[' {
		return 0, false, false
	}
	end := skipString(s, i)
	if end < 0 {
		return 0, false, false
	}
	colon := skipBlank(s, end)
	if colon >= len(s) || s[colon] != ':' {
		return 0, false, false
	}
	value := skipBlank(s, colon+1)
	if wrap = value < len(s) && s[value] == '['; wrap {
		value++ // the wrapper bracket is consumed here, not by the scan
	}
	return value - 1, wrap, true
}

// skipString returns the index just past the string literal starting at
// i, or -1 when it never closes (the repair then leaves the bytes alone
// and the strict decode reports the real damage).
func skipString(s string, i int) int {
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++ // the escaped byte can only have been ' or \.
		case '"':
			return j + 1
		}
	}
	return -1
}

// skipBlank returns the first non-blank index at or after i (len(s) when
// the rest is blank).
func skipBlank(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	return i
}

// followedByCloser reports whether the next non-blank byte closes the
// current object or array — the trailing-comma shape repairJSON drops.
func followedByCloser(s string, i int) bool {
	j := skipBlank(s, i+1)
	return j < len(s) && (s[j] == '}' || s[j] == ']')
}
