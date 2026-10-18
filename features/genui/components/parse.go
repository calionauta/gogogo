// SCOPE:plugin — the parse front door, split from catalog.go for the
// 500-line file gate (same split as prompts.go): the registry decides what
// RENDERS, this file turns a raw model answer into directives against it.
package components

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// stripFences removes ```json fences models wrap answers in. Duplicates
// three lines instead of importing an unexported helper: the catalog
// owns its front door (same reason parseStringArray owns its own).
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if i := strings.Index(s, "\n"); i > 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, "```"); i > 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// rawDirective mirrors the model JSON including nesting; converted to
// Directive only after validation passes at every level.
//
// Children and Components are the same slot under two names: the prompt
// asks for "children", and models answer with the envelope's own
// "components" (the wrapper key they just saw) at least as often. Both
// resolve to nested directives; accepting only one silently dropped the
// children (a section rendered empty instead of erroring).
type rawDirective struct {
	Type       string         `json:"type"`
	Props      map[string]any `json:"props"`
	Children   []rawDirective `json:"children,omitempty"`
	Components []rawDirective `json:"components,omitempty"`
}

// ParseDirectives extracts and validates the component list from raw
// model output. Tolerant of fences, surrounding prose and the structural
// slips small models make, strict about the catalog: one unknown type,
// undecodable props, over-deep nesting or empty answer rejects the whole
// answer (a half-rendered generative region is worse than an error toast
// with retry).
//
// Strict JSON is tried first, so a well-formed answer decodes exactly as
// encoding/json would decode it and no tolerance can bend it. Only when
// that fails does the tolerant reader take over, and it feeds the SAME
// catalog validation — it can recover a shape, never invent a component.
func ParseDirectives(raw string) ([]Directive, error) {
	body := stripFences(strings.TrimSpace(raw))
	if dirs, err := parseStrict(body); err == nil {
		return dirs, nil
	}
	return parseTolerant(body)
}

// parseStrict is the well-formed path: the answer's outer object, decoded
// by encoding/json and converted.
func parseStrict(body string) ([]Directive, error) {
	start, end := strings.IndexByte(body, '{'), strings.LastIndexByte(body, '}')
	if start < 0 || end <= start {
		return nil, errors.New("genui: no JSON object found")
	}
	var envelope struct {
		Components []rawDirective `json:"components"`
	}
	if err := json.Unmarshal([]byte(body[start:end+1]), &envelope); err != nil {
		return nil, fmt.Errorf("genui: decode directives: %w", err)
	}
	return convertAll(envelope.Components)
}

// convertAll validates a directive list, stopping at the first failure.
// Both parse paths end here, which is what keeps them from disagreeing
// about what the catalog accepts.
func convertAll(raw []rawDirective) ([]Directive, error) {
	if len(raw) == 0 {
		return nil, errors.New("genui: no components in answer")
	}
	dirs := make([]Directive, 0, len(raw))
	for _, c := range raw {
		d, err := convertDirective(c, 0)
		if err != nil {
			return nil, err
		}
		dirs = append(dirs, d)
	}
	return dirs, nil
}

// childrenFromProps bridges the model's favorite nesting mistake:
// children tucked inside props instead of beside type/props, under
// "children" or under "components" (the envelope key it just saw). It
// round-trips the value through rawDirective so malformed entries fail
// here instead of halfway through validation, and removes the key so strict
// prop decoding still passes.
//
// A present-but-unusable value is an ERROR here, and that is this path's
// deliberate half of the strict/tolerant split: strict JSON is only tried
// first, so a shape this reader rejects is still handed to the tolerant one,
// which SALVAGES it (toRaw drops a scalar children and keeps the rest of the
// component — see fromObject). Erroring explicitly rather than leaving the
// key for the props decoder means the message names props.children, which is
// the field the model actually got wrong, instead of "unknown field
// children" from a struct that was never meant to carry it.
func childrenFromProps(props map[string]any) ([]rawDirective, bool, error) {
	for _, key := range []string{"children", "components"} {
		raw, ok := props[key]
		if !ok || raw == nil {
			continue
		}
		delete(props, key)
		blob, err := json.Marshal(raw)
		if err != nil {
			return nil, false, fmt.Errorf("genui: encode props.%s: %w", key, err)
		}
		var out []rawDirective
		dec := json.NewDecoder(strings.NewReader(string(blob)))
		dec.DisallowUnknownFields()
		if derr := dec.Decode(&out); derr != nil {
			return nil, false, fmt.Errorf("genui: props.%s is not a component list: %w", key, derr)
		}
		return out, true, nil
	}
	return nil, false, nil
}

// convertDirective validates one node (type registered, props decode,
// depth budget) and its subtree. Render doubles as the validator so
// parse and render can never disagree on what is legal.
func convertDirective(c rawDirective, depth int) (Directive, error) {
	if depth > maxDirectiveDepth {
		return Directive{}, fmt.Errorf("genui: directives nested deeper than %d", maxDirectiveDepth)
	}
	if !Registered(c.Type) {
		return Directive{}, fmt.Errorf("genui: unregistered component %q", c.Type)
	}
	// Canonicalize a panel alias (`todos` → `todo_list`) before anything else
	// looks at the name: models write the plural or a synonym often enough that
	// rejecting it cost the user the whole answer (the panel was repaired, then
	// dropped, and the bubble came back empty). Aliases are a spelling fix —
	// only compiled-in entries resolve, so an unknown name still fails closed.
	c.Type = ResolvePanelName(c.Type)
	// Models habitually nest children inside props (the envelope shape
	// puts everything under "props"); bridge them to the directive
	// level (childrenFromProps drops the key, so strict prop decoding
	// still applies to what is left).
	rawChildren := append(append([]rawDirective(nil), c.Children...), c.Components...)
	if len(rawChildren) == 0 {
		bridged, ok, err := childrenFromProps(c.Props)
		if err != nil {
			return Directive{}, err
		}
		if ok {
			rawChildren = bridged
		}
	}
	children := make([]Directive, 0, len(rawChildren))
	for _, child := range rawChildren {
		converted, err := convertDirective(child, depth+1)
		if err != nil {
			return Directive{}, err
		}
		children = append(children, converted)
	}
	d := Directive{Component: c.Type, Props: c.Props, Children: children}
	// Panels are checked BEFORE the renderer, and on both parse paths (they
	// all end here): a model answer that supplies a server-owned field is a
	// shape error to repair, not something hydration silently overwrites.
	if err := validatePanelProps(c.Type, d.Props); err != nil {
		return Directive{}, err
	}
	if _, err := renderDepth(d, depth); err != nil {
		return Directive{}, err
	}
	return d, nil
}
