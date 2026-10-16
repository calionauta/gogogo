// SCOPE:plugin — the tolerant front door of the component catalog.
package components

import (
	"errors"
	"maps"
	"strconv"
	"strings"
)

// maxReaderDepth bounds how far the tolerant reader descends. The catalog
// itself allows three levels (maxDirectiveDepth); this is the reader's own
// stack guard against attacker-shaped input (a wall of '['), not a catalog
// rule.
const maxReaderDepth = 32

// reader walks the model's answer as a JSON-ish token stream instead of
// trusting it to BE JSON.
//
// One mechanism replaces a pile of string repairs, because small models
// emit the same structural slips over and over and each one used to cost a
// repair of its own: the envelope key repeated where an array element
// belongs, a section's children parked inside "props", a closer that does
// not match the open bracket, a comma or colon between nothing, output cut
// off mid-answer. Those are one shape — "JSON-ish" — so they get one
// tolerant reader, and the guarantee that matters is unchanged: the result
// still goes through the catalog, so a component the registry does not know
// rejects the whole answer.
//
// Nothing here errors. Reading is best-effort; the strict catalog
// validation afterwards decides what is acceptable, and an answer that
// survives reading without a single registered component is rejected there
// ("no components in answer") rather than here.
type reader struct {
	src   string
	pos   int
	depth int
}

// value reads one value: object, array, string, or scalar. Past the depth
// cap it stops reading and consumes the rest of the input: the enclosing
// loops exit on end-of-input, and anything nested deeper than the cap is
// not a catalog component anyway.
func (r *reader) value() any {
	r.skipSpace()
	if r.pos >= len(r.src) {
		return nil
	}
	if r.depth >= maxReaderDepth {
		r.pos = len(r.src)
		return nil
	}
	r.depth++
	defer func() { r.depth-- }()
	switch r.src[r.pos] {
	case '{':
		return r.object()
	case '[':
		return r.array()
	case '"':
		return r.str()
	default:
		return r.scalar()
	}
}

// object reads a keyed value. Unkeyed values are read and dropped, any
// closer closes it (bracket confusion is common), and end-of-input returns
// what arrived — a truncated answer is still worth rendering.
func (r *reader) object() map[string]any {
	r.pos++ // '{'
	out := map[string]any{}
	for {
		r.skipSpace()
		if r.pos >= len(r.src) {
			return out
		}
		switch r.src[r.pos] {
		case '}', ']':
			r.pos++
			return out
		case ',':
			r.pos++
		case '"':
			key := r.str()
			r.skipSpace()
			if r.pos >= len(r.src) || r.src[r.pos] != ':' {
				continue // a string with no value: noise, not a key
			}
			r.pos++              // ':'
			out[key] = r.value() // last key wins, like encoding/json
		default:
			r.value() // unkeyed value: consume it and ignore it
		}
	}
}

// array reads elements until a closer or the end. A repeated wrapper key
// needs no special case here: its label reads as a string element and its
// colon as an unreadable scalar, and toRaw drops both scalars and splices
// the array that follows.
func (r *reader) array() []any {
	r.pos++ // '['
	out := []any{}
	for {
		r.skipSpace()
		if r.pos >= len(r.src) {
			return out
		}
		switch r.src[r.pos] {
		case ']', '}':
			r.pos++
			return out
		case ',':
			r.pos++
		default:
			out = append(out, r.value())
		}
	}
}

// str reads a string literal, unescaping it when it is well formed. An
// unterminated literal keeps the bytes that arrived (truncated answers).
func (r *reader) str() string {
	start := r.pos
	for i := start + 1; i < len(r.src); i++ {
		switch r.src[i] {
		case '\\':
			i++ // the escaped byte can only follow a backslash
		case '"':
			lit := r.src[start : i+1]
			r.pos = i + 1
			if s, err := strconv.Unquote(lit); err == nil {
				return s
			}
			return lit[1 : len(lit)-1]
		}
	}
	r.pos = len(r.src)
	if start+1 <= len(r.src) {
		return r.src[start+1:]
	}
	return ""
}

// scalar reads a bare token: a number, a boolean, null, or something
// neither (returned as the raw string, so the type mismatch is the prop
// decoder's call, not a parse failure). A lone delimiter advances one byte
// and yields nil, which keeps every loop in this file progressing.
func (r *reader) scalar() any {
	start := r.pos
	for r.pos < len(r.src) && !strings.ContainsRune(",}]:{[ \t\r\n", rune(r.src[r.pos])) {
		r.pos++
	}
	if r.pos == start {
		r.pos++
		return nil
	}
	tok := r.src[start:r.pos]
	switch tok {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	if f, err := strconv.ParseFloat(tok, 64); err == nil {
		return f
	}
	return tok
}

func (r *reader) skipSpace() {
	for r.pos < len(r.src) {
		switch r.src[r.pos] {
		case ' ', '\t', '\n', '\r':
			r.pos++
		default:
			return
		}
	}
}

// parseTolerant reads the answer, then validates every component it found
// against the catalog (same conversion as the strict path, so the two can
// never disagree on what is legal).
func parseTolerant(body string) ([]Directive, error) {
	start := strings.IndexByte(body, '{')
	if start < 0 {
		return nil, errors.New("genui: no JSON object found")
	}
	r := &reader{src: body, pos: start}
	root, ok := r.value().(map[string]any)
	if !ok {
		return nil, errors.New("genui: no JSON object found")
	}
	return convertAll(toRaw(root["components"]))
}

// toRaw lifts every component object out of the tolerant tree. Arrays
// splice — a repeated wrapper key leaves its value as a nested array
// element whose entries belong to the enclosing list — and scalars (the
// stray label itself, a misplaced comma) contribute nothing.
func toRaw(v any) []rawDirective {
	switch t := v.(type) {
	case []any:
		out := make([]rawDirective, 0, len(t))
		for _, e := range t {
			out = append(out, toRaw(e)...)
		}
		return out
	case map[string]any:
		return []rawDirective{fromObject(t)}
	default:
		return nil
	}
}

// fromObject builds one directive from a node: its type, its props, and its
// children wherever the model put them. Children arrive under "children" or
// under "components" — the envelope key the model just read — either beside
// the props or inside them (where the envelope shape invites everything).
// The child keys are removed from props so the catalog's strict prop
// decoding still applies to what is left.
func fromObject(o map[string]any) rawDirective {
	d := rawDirective{Type: asString(o["type"])}
	if props, ok := o["props"].(map[string]any); ok {
		d.Props = make(map[string]any, len(props))
		maps.Copy(d.Props, props)
	} else {
		d.Props = map[string]any{}
	}
	for _, key := range []string{"children", "components"} {
		kids := o[key]
		if kids == nil {
			kids = d.Props[key]
		}
		d.Children = append(d.Children, toRaw(kids)...)
		delete(d.Props, key)
	}
	return d
}

// asString reads a node's type field; anything else is the empty string,
// which the registry rejects (fail closed).
func asString(v any) string {
	s, _ := v.(string)
	return s
}
