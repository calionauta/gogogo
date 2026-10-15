// SCOPE:plugin — gogen-ui component catalog.
package components

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/a-h/templ"
)

// Directive is one model-emitted component: a registry name plus its
// props. The model never emits markup — only names from Registered()
// with props that decode into the structs below. Anything else fails
// closed at ParseDirectives, before any renderer runs.
//
// Children enables one level of composition: container components
// (section) nest directives inside directives, so answers can group
// (a week plan holding its table) instead of only stacking siblings.
// Depth is capped (maxDirectiveDepth) — unbounded recursion on
// attacker-shaped input is a stack-exhaustion vector.
type Directive struct {
	Component string
	Props     map[string]any
	Children  []Directive
}

// maxDirectiveDepth bounds nesting (top level counts as 0).
const maxDirectiveDepth = 3

// TextNoteProps is a single prose card.
type TextNoteProps struct {
	Text string `json:"text"`
}

// SectionProps titles a group of nested components.
type SectionProps struct {
	Title string `json:"title"`
}

// Bar is one labeled bar in a bar chart.
type Bar struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

// BarChartProps is a pure-CSS bar chart: zero JS, zero chart lib, fully
// server-rendered — the visual proof an answer is interface, not prose.
type BarChartProps struct {
	Title string `json:"title"`
	Bars  []Bar  `json:"bars"`
}

// Action is one app-mutating button: a form posting fields to a
// same-origin URL. This is what makes an answer act on your data
// instead of describing it — the un-chatbot primitive.
type Action struct {
	Label  string            `json:"label"`
	Method string            `json:"method"`
	URL    string            `json:"url"`
	Fields map[string]string `json:"fields,omitempty"`
}

// ActionButtonsProps is a row of action buttons.
type ActionButtonsProps struct {
	Actions []Action `json:"actions"`
}

// HTMLBlockProps is model-composed visual markup for answers the fixed
// catalog cannot express (a timeline, a comparison, a dashboard invented
// for this request). The HTML is sanitized at parse time — the model
// styles with Tailwind/DaisyUI classes, never scripts, styles, handlers
// or media. Chat-only affordance: the Ask prompt never advertises it.
type HTMLBlockProps struct {
	HTML string `json:"html"`
}

// Plan is one actionable plan card.
type Plan struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// PlanCardsProps is a list of plan cards with apply buttons.
type PlanCardsProps struct {
	Plans []Plan `json:"plans"`
}

// DataTableProps is a small read-only table.
type DataTableProps struct {
	Headers []string   `json:"headers"`
	Rows    [][]string `json:"rows"`
}

// renderer decodes untrusted props into a component or fails.
type renderer func(map[string]any) (templ.Component, error)

func decodeProps[T any](props map[string]any) (T, error) {
	var out T
	raw, err := json.Marshal(props)
	if err != nil {
		return out, fmt.Errorf("genui: encode props: %w", err)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, fmt.Errorf("genui: decode props: %w", err)
	}
	return out, nil
}

// renderers is the catalog: every name the prompt advertises resolves
// here, or ParseDirectives rejects the whole answer. Add a component by
// adding one entry plus its Templ renderer — the prompt, validation and
// tests follow from this map (see Registered, PromptForCatalog).
var renderers = map[string]renderer{
	"text_note": func(props map[string]any) (templ.Component, error) {
		p, err := decodeProps[TextNoteProps](props)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.Text) == "" {
			return nil, errors.New("genui: text_note requires text")
		}
		return GenuiTextNote(p.Text), nil
	},
	"plan_cards": func(props map[string]any) (templ.Component, error) {
		p, err := decodeProps[PlanCardsProps](props)
		if err != nil {
			return nil, err
		}
		if len(p.Plans) == 0 {
			return nil, errors.New("genui: plan_cards requires at least one plan")
		}
		for _, plan := range p.Plans {
			if strings.TrimSpace(plan.Title) == "" {
				return nil, errors.New("genui: plan_cards plan requires title")
			}
		}
		return GenuiPlanCards(p.Plans), nil
	},
	"data_table": func(props map[string]any) (templ.Component, error) {
		p, err := decodeProps[DataTableProps](props)
		if err != nil {
			return nil, err
		}
		if len(p.Headers) == 0 || len(p.Rows) == 0 {
			return nil, errors.New("genui: data_table requires headers and rows")
		}
		return GenuiDataTable(p.Headers, p.Rows), nil
	},
	"bar_chart": func(props map[string]any) (templ.Component, error) {
		p, err := decodeProps[BarChartProps](props)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.Title) == "" {
			return nil, errors.New("genui: bar_chart requires title")
		}
		if len(p.Bars) == 0 {
			return nil, errors.New("genui: bar_chart requires at least one bar")
		}
		peak := 0.0
		for _, b := range p.Bars {
			if b.Value < 0 {
				return nil, fmt.Errorf("genui: bar_chart bar %q has negative value", b.Label)
			}
			peak = max(peak, b.Value)
		}
		if peak <= 0 {
			return nil, errors.New("genui: bar_chart needs a positive value to scale by")
		}
		return GenuiBarChart(p.Title, p.Bars, peak), nil
	},
	"action_buttons": func(props map[string]any) (templ.Component, error) {
		p, err := decodeProps[ActionButtonsProps](props)
		if err != nil {
			return nil, err
		}
		if len(p.Actions) == 0 {
			return nil, errors.New("genui: action_buttons requires at least one action")
		}
		for _, a := range p.Actions {
			if err := checkAction(a); err != nil {
				return nil, err
			}
		}
		return GenuiActionButtons(p.Actions), nil
	},
	"html_block": func(props map[string]any) (templ.Component, error) {
		p, err := decodeProps[HTMLBlockProps](props)
		if err != nil {
			return nil, err
		}
		clean, err := SanitizeHTML(p.HTML)
		if err != nil {
			return nil, err
		}
		return GenuiHTMLBlock(clean), nil
	},
}

// checkAction enforces the same-origin contract: action buttons post
// to this app only. Absolute URLs, protocol-relative URLs, schemes and
// whitespace never reach a form — a model turning answers into request
// forgery buttons fails here, not in the browser.
func checkAction(a Action) error {
	if strings.TrimSpace(a.Label) == "" {
		return errors.New("genui: action requires label")
	}
	switch a.Method {
	case http.MethodPost, http.MethodGet:
	default:
		return fmt.Errorf("genui: action method %q must be POST or GET", a.Method)
	}
	u := a.URL
	if u == "" || !strings.HasPrefix(u, "/") || strings.HasPrefix(u, "//") ||
		strings.ContainsAny(u, " \t\n:") {
		return fmt.Errorf("genui: action url %q must be a same-origin path", u)
	}
	return nil
}

// Registered reports whether name is a catalog component. Section is
// a container, not a leaf renderer: it resolves here so prompts,
// validation and tests name one registry, while Render handles its
// children natively below.
func Registered(name string) bool {
	if name == "section" {
		return true
	}
	_, ok := renderers[name]
	return ok
}

// Render resolves one validated directive to its component. Unknown
// names fail even if ParseDirectives was skipped — defense in depth,
// same fail-closed shape as RequireOwner behind every handler.
func Render(d Directive) (templ.Component, error) {
	return renderDepth(d, 0)
}

// RenderAll renders a directive list, stopping at the first failure
// (a half-rendered generative region is worse than an error toast).
func RenderAll(dirs []Directive) ([]templ.Component, error) {
	comps := make([]templ.Component, 0, len(dirs))
	for _, d := range dirs {
		comp, err := Render(d)
		if err != nil {
			return nil, err
		}
		comps = append(comps, comp)
	}
	return comps, nil
}

// renderDepth is Render with a nesting budget: one enforcement point
// for both the parse path and direct Render callers.
func renderDepth(d Directive, depth int) (templ.Component, error) {
	if depth > maxDirectiveDepth {
		return nil, fmt.Errorf("genui: directives nested deeper than %d", maxDirectiveDepth)
	}
	if d.Component == "section" {
		var title SectionProps
		raw, err := json.Marshal(d.Props)
		if err != nil {
			return nil, fmt.Errorf("genui: encode section props: %w", err)
		}
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&title); err != nil {
			return nil, fmt.Errorf("genui: decode section props: %w", err)
		}
		if strings.TrimSpace(title.Title) == "" {
			return nil, errors.New("genui: section requires title")
		}
		children := make([]templ.Component, 0, len(d.Children))
		for _, child := range d.Children {
			comp, err := renderDepth(child, depth+1)
			if err != nil {
				return nil, err
			}
			children = append(children, comp)
		}
		return GenuiSection(title.Title, children), nil
	}
	r, ok := renderers[d.Component]
	if !ok {
		return nil, fmt.Errorf("genui: unregistered component %q", d.Component)
	}
	props := d.Props
	if props == nil {
		props = map[string]any{}
	}
	return r(props)
}

// PromptForCatalog generates the model instructions from the registry
// (OpenUI pattern): the prompt advertises exactly the names that
// resolve, so the model is never invited to emit components that fail
// closed at parse time.
func PromptForCatalog() string {
	return "Answer with a single JSON object shaped " +
		`{"components":[{"type":"<name>","props":{...}}]} ` +
		"using ONLY these components: " +
		"text_note {text}, " +
		"plan_cards {plans:[{title, detail}]}, " +
		"data_table {headers:[], rows:[[]]}, " +
		"bar_chart {title, bars:[{label, value>=0}]}, " +
		"action_buttons {actions:[{label, method:POST|GET, url:/same/origin/path, fields:{}}]}, " +
		"section {title} holding nested components. " +
		"No prose outside the JSON object."
}

// PromptForChat is the chat affordance: the same structured catalog
// plus html_block, with the model restricted to answering visually.
// html_block carries Tailwind/DaisyUI markup the model composes for
// THIS request (a timeline, a comparison, a mini dashboard) when no
// catalog component fits — sanitized server-side, so scripts, styles,
// handlers and media never reach the browser. The Ask demo keeps
// PromptForCatalog (catalog-only is its showcase); chat gets the
// invented-visual path because conversation, not decision, is its job.
func PromptForChat() string {
	return "Answer with a single JSON object shaped " +
		`{"components":[{"type":"<name>","props":{...}}]} ` +
		"using ONLY these components: " +
		"text_note {text}, " +
		"plan_cards {plans:[{title, detail}]}, " +
		"data_table {headers:[], rows:[[]]}, " +
		"bar_chart {title, bars:[{label, value>=0}]}, " +
		"action_buttons {actions:[{label, method:POST|GET, url:/same/origin/path, fields:{}}]}, " +
		"html_block {html: Tailwind/DaisyUI markup string invented for this answer}, " +
		"section {title} holding nested components. " +
		"Present the information in the most adequate visual form for the request: " +
		"prefer a visual component over prose, and use html_block when the fixed " +
		"catalog cannot express it. The html must be display markup only — no " +
		"script, style, event handlers, forms or media. " +
		"No prose outside the JSON object."
}

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
type rawDirective struct {
	Type     string         `json:"type"`
	Props    map[string]any `json:"props"`
	Children []rawDirective `json:"children,omitempty"`
}

// ParseDirectives extracts and validates the component list from raw
// model output. Tolerant of fences and surrounding prose, strict about
// the catalog: one unknown type, undecodable props, over-deep nesting
// or empty answer rejects the whole answer (a half-rendered generative
// region is worse than an error toast with retry).
func ParseDirectives(raw string) ([]Directive, error) {
	s := stripFences(strings.TrimSpace(raw))
	start, end := strings.IndexByte(s, '{'), strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return nil, errors.New("genui: no JSON object found")
	}
	body := repairJSON(s[start : end+1])
	var envelope struct {
		Components []rawDirective `json:"components"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		return nil, fmt.Errorf("genui: decode directives: %w", err)
	}
	if len(envelope.Components) == 0 {
		return nil, errors.New("genui: no components in answer")
	}
	dirs := make([]Directive, 0, len(envelope.Components))
	for _, c := range envelope.Components {
		d, err := convertDirective(c, 0)
		if err != nil {
			return nil, err
		}
		dirs = append(dirs, d)
	}
	return dirs, nil
}

// childrenFromProps bridges the model's favorite nesting mistake:
// children tucked inside props instead of beside type/props. It
// round-trips the value through rawDirective so malformed entries fail
// here (ok=false) instead of halfway through validation.
func childrenFromProps(props map[string]any) ([]rawDirective, bool) {
	raw, ok := props["children"]
	if !ok || raw == nil {
		return nil, false
	}
	blob, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}
	var out []rawDirective
	dec := json.NewDecoder(strings.NewReader(string(blob)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return nil, false
	}
	return out, true
}

// repairJSON strips trailing commas before } and ] — the most common
// small-model malformation. The scan respects string literals (a comma
// inside "a, b" or an escaped quote never triggers), so valid JSON
// passes through byte-identical and strictness is otherwise unchanged.
func repairJSON(s string) string {
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

// followedByCloser reports whether the next non-blank byte closes the
// current object or array — the trailing-comma shape repairJSON drops.
func followedByCloser(s string, i int) bool {
	j := i + 1
	for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
		j++
	}
	return j < len(s) && (s[j] == '}' || s[j] == ']')
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
	// Models habitually nest children inside props (the envelope shape
	// puts everything under "props"); bridge them to the directive
	// level and drop the key so strict prop decoding still passes.
	rawChildren := c.Children
	if len(rawChildren) == 0 {
		if bridged, ok := childrenFromProps(c.Props); ok {
			rawChildren = bridged
			delete(c.Props, "children")
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
	if _, err := renderDepth(d, depth); err != nil {
		return Directive{}, err
	}
	return d, nil
}
