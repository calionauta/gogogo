// SCOPE:feature — gogen-ui component catalog.
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

// sectionComponent is the catalog container: prompts, validation and
// renderers name it in one spelling so the registry never drifts.
const sectionComponent = "section"

// Registered reports whether name is a catalog component. Section is
// a container, not a leaf renderer: it resolves here so prompts,
// validation and tests name one registry, while Render handles its
// children natively below.
func Registered(name string) bool {
	if name == sectionComponent {
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
	if d.Component == sectionComponent {
		var title SectionProps
		raw, err := json.Marshal(d.Props)
		if err != nil {
			return nil, fmt.Errorf("genui: encode section props: %w", err)
		}
		// Lenient on purpose: props-nested children are lifted before
		// validation, but older payloads may still carry them; only
		// the title matters here.
		if err := json.Unmarshal(raw, &title); err != nil {
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
		"section {title} with nested \"children\": " +
		`{"type":"section","props":{"title":"..."},"children":[...]}. ` +
		"Never put components inside props. " +
		"No prose outside the JSON object."
}

// stripFences removes ```json fences models wrap answers in. Handles
// fences wrapping the answer anywhere in the output ("Here you go:\n
// ```json\n{...}\n```\nHope that helps!"): when a fence block opens
// BEFORE the first "{" the first fenced block wins, otherwise the
// input is returned trimmed. The ordering guard matters: fence runs
// inside a props string (a text_note showing a code sample) must never
// be treated as wrapping — that mangled valid JSON into a fragment.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	fence := strings.Index(s, "```")
	if fence < 0 {
		return s
	}
	if brace := strings.IndexByte(s, '{'); brace >= 0 && brace < fence {
		return s
	}
	rest := s[fence+3:]
	if nl := strings.Index(rest, "\n"); nl >= 0 {
		rest = rest[nl+1:]
	}
	if end := strings.Index(rest, "```"); end >= 0 {
		return strings.TrimSpace(rest[:end])
	}
	if strings.HasPrefix(s, "```") {
		return strings.TrimSpace(rest)
	}
	return s
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
//
// Tolerance details (real-model output, not test fixtures): the first
// JSON object is decoded with json.Decoder, so trailing garbage — an
// extra "}" from overshot brace balancing, a echoed second object,
// trailing prose — never fails the whole answer. A leading "Here you
// go:" plus fences is stripped by stripFences first.
func ParseDirectives(raw string) ([]Directive, error) {
	s := stripFences(strings.TrimSpace(raw))
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return nil, errors.New("genui: no JSON object found")
	}
	var envelope struct {
		Components []rawDirective `json:"components"`
	}
	dec := json.NewDecoder(strings.NewReader(s[start:]))
	if err := dec.Decode(&envelope); err != nil {
		// Decoder already tolerates trailing bytes; a failure here is a
		// genuinely malformed object. Fall back to the legacy greedy
		// first-{..last-} slice for inputs where a leading "{" belongs
		// to prose (e.g. emoticons) and the real object starts later.
		end := strings.LastIndexByte(s, '}')
		if end <= start {
			return nil, errors.New("genui: no JSON object found")
		}
		if err2 := json.Unmarshal([]byte(s[start:end+1]), &envelope); err2 != nil {
			return nil, fmt.Errorf("genui: decode directives: %w", err)
		}
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

// convertDirective validates one node (type registered, props decode,
// depth budget) and its subtree. Render doubles as the validator so
// parse and render can never disagree on what is legal.
//
// Section nesting accepts two wire shapes: canonical "children" and
// the "components"/"children" array nested inside props (models emit
// the latter when the prompt says "section holding nested
// components" — see the plan-my-day report). Props-nested lists are
// lifted into Children and removed from Props before validation, so
// DisallowUnknownFields never rejects them. Lifting applies to section
// only: leaf components stay fail-closed on unknown props, so a model
// smuggling keys into text_note still rejects instead of silently
// dropping data.
func convertDirective(c rawDirective, depth int) (Directive, error) {
	if depth > maxDirectiveDepth {
		return Directive{}, fmt.Errorf("genui: directives nested deeper than %d", maxDirectiveDepth)
	}
	if !Registered(c.Type) {
		return Directive{}, fmt.Errorf("genui: unregistered component %q", c.Type)
	}
	if c.Type == sectionComponent && c.Props != nil {
		if nested, ok := nestedChildren(c.Props); ok {
			c.Props = withoutNestedKeys(c.Props)
			c.Children = append(c.Children, nested...)
		}
	}
	children := make([]Directive, 0, len(c.Children))
	for _, child := range c.Children {
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

// nestedChildren lifts a components/children array nested inside props
// (the shape models emit for sections) into raw directives. Returns
// ok=false when no such key exists.
func nestedChildren(props map[string]any) ([]rawDirective, bool) {
	for _, key := range []string{"components", "children"} {
		v, ok := props[key]
		if !ok {
			continue
		}
		raw, err := json.Marshal(v)
		if err != nil {
			continue
		}
		var nested []rawDirective
		if err := json.Unmarshal(raw, &nested); err != nil || len(nested) == 0 {
			continue
		}
		return nested, true
	}
	return nil, false
}

// withoutNestedKeys returns props minus the lifted nesting keys, so
// strict prop decoders never see them.
func withoutNestedKeys(props map[string]any) map[string]any {
	out := make(map[string]any, len(props))
	for k, v := range props {
		if k == "components" || k == "children" {
			continue
		}
		out[k] = v
	}
	return out
}
