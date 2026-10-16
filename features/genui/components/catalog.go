// SCOPE:plugin — gogen-ui component catalog.
package components

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
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

// TodoListProps is the model's view of the app's OWN todo list — and the
// reason it is a filter and not content: a system component shows real
// state, so the model may only choose WHICH slice to show. Rows are filled
// by the worker's hydration and never come from the model
// (decodeProps' DisallowUnknownFields is what rejects an answer that
// tries to supply them). Filter is a closed set; an open string here
// would let the model invent a query the store cannot answer.
type TodoListProps struct {
	Filter string    `json:"filter"`
	Rows   []TodoRow `json:"rows,omitempty"`
}

// TodoRow is one hydrated row: the app's truth, filled server-side.
type TodoRow struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

// TodoListFilters is the closed set the filter prop accepts, in prompt
// order. Named, not numbered: the store is asked for the named slice, and a
// panel's prompt fragment is built from this list so the advertised filters
// cannot drift from the accepted ones.
var TodoListFilters = []string{"open", "done", "all"}

// TodoFiltersContain reports whether a filter is in the closed set. Exported
// so a panel implementation validates against the same list the catalog does.
func TodoFiltersContain(filter string) bool {
	return slices.Contains(TodoListFilters, filter)
}

// todoListName is the app's own panel component: chat's tool-based answer
// ("show me my todos") as opposed to a catalog primitive.
const todoListName = "todo_list"

// chatOnlyComponents are advertised in chat's prompt and never in Ask's: the
// invented-markup escape hatch and the app's own panels. Chat's prompt is
// where the model is told they exist; Ask never names them, and the router's
// candidate set is derived from what Ask advertises, so a classifier pick
// cannot reach them either (see CatalogComponents).
func chatOnlyComponents(name string) bool {
	return name == htmlBlockName || IsPanel(name)
}

// renderer decodes untrusted props into a component or fails.
type renderer func(map[string]any) (templ.Component, error)

// sectionName is the one container component: it resolves like a leaf
// (see Registered) and renders its children instead of props.
const sectionName = "section"

// htmlBlockName is chat's invented-markup affordance. Ask never
// advertises it (see PromptForChat vs PromptForCatalog), so anything that
// chooses a component for Ask must treat this name as out of catalog.
const htmlBlockName = "html_block"

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
	todoListName: func(props map[string]any) (templ.Component, error) {
		p, err := decodeProps[TodoListProps](props)
		if err != nil {
			return nil, err
		}
		if !TodoFiltersContain(p.Filter) {
			return nil, fmt.Errorf("genui: todo_list filter %q is not one of %v", p.Filter, TodoListFilters)
		}
		return GenuiTodoList(p.Filter, p.Rows), nil
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
	htmlBlockName: func(props map[string]any) (templ.Component, error) {
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
	if name == sectionName {
		return true
	}
	_, ok := renderers[name]
	return ok
}

// CatalogComponents lists the names an Ask answer may lead with: every
// leaf renderer plus the section container, sorted, minus html_block.
// html_block is chat's affordance — Ask never advertises it, so the
// router must never offer it as a label (a pick outside this set is
// ignored, which is what keeps a router on but wrong from changing the
// prompt). Derived from the registry, not spelled out: a new component
// becomes routable by adding its one map entry.
func CatalogComponents() []string {
	names := make([]string, 0, len(renderers)+1)
	for name := range renderers {
		if chatOnlyComponents(name) {
			continue
		}
		names = append(names, name)
	}
	names = append(names, sectionName)
	slices.Sort(names)
	return names
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
	if d.Component == sectionName {
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
