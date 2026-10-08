// SCOPE:layer=infra,removal=plugin — Loro Text ops for server-owned text docs (notes)
package collab

import (
	"fmt"
	"strings"

	loro "github.com/aholstenson/loro-go"
)

// textBody is the root Loro Text container holding a note's characters.
// One container per Doc: a notes doc holds exactly one text.
const textBody = "body"

// TextOp is a single character-level mutation a browser sends. The server
// serializes ops under the doc mutex, so positions are always evaluated
// against current state — concurrent inserts merge instead of clobbering.
//
// Positions are Unicode scalar values. ASCII and BMP text — the notes use
// case — are exact; JS non-BMP characters (surrogate pairs) may drift by
// one position under concurrency. Rich-text marks are ignored on read.
type TextOp struct {
	Type   string `json:"t"` // "ins" | "del"
	Index  uint32 `json:"i"`
	Text   string `json:"s,omitempty"` // ins payload
	Length uint32 `json:"n,omitempty"` // del length
}

// ApplyTextOp applies one op and returns the resolved text. Insert clamps
// to the end (positions shift under concurrency, so overflow means
// append); delete clamps to the available range. Unknown types are an
// error — the caller answers 400.
func (d *Doc) ApplyTextOp(op TextOp) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	t := d.loro.GetText(loro.AsContainerId(textBody))
	switch op.Type {
	case "ins":
		if err := t.Insert(min(op.Index, t.LenUnicode()), op.Text); err != nil {
			return "", err
		}
	case "del":
		length := t.LenUnicode()
		if op.Index >= length || op.Length == 0 {
			return d.textLocked(), nil
		}
		if err := t.Delete(op.Index, min(op.Length, length-op.Index)); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unknown text op %q", op.Type)
	}
	d.loro.Commit()
	return d.textLocked(), nil
}

// Text returns the current resolved plain text (marks ignored).
func (d *Doc) Text() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.textLocked()
}

// textLocked concatenates the richtext segments into plain text.
func (d *Doc) textLocked() string {
	t := d.loro.GetText(loro.AsContainerId(textBody))
	list, ok := asValueList(t.GetRichtextValue())
	if !ok {
		return ""
	}
	var sb strings.Builder
	for _, item := range list.Value {
		m, ok := asValueMap(item)
		if !ok {
			continue
		}
		if ins, ok := m.Value["insert"].(loro.LoroValueString); ok {
			sb.WriteString(ins.Value)
		}
	}
	return sb.String()
}

func asValueList(v loro.LoroValue) (loro.LoroValueList, bool) {
	if l, ok := v.(loro.LoroValueList); ok {
		return l, true
	}
	if l, ok := v.(*loro.LoroValueList); ok && l != nil {
		return *l, true
	}
	return loro.LoroValueList{}, false
}

func asValueMap(v loro.LoroValue) (loro.LoroValueMap, bool) {
	if m, ok := v.(loro.LoroValueMap); ok {
		return m, true
	}
	if m, ok := v.(*loro.LoroValueMap); ok && m != nil {
		return *m, true
	}
	return loro.LoroValueMap{}, false
}
