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

// catalogLeafNames is the shared catalog band: one spelling per component, so a
// rename cannot land in the registry but miss the prompt (or the reverse).
// Declared one per line on purpose — a const BLOCK gets its '=' signs aligned
// by gofumpt, which reads like an env file to the secret scanner.
const (
	textNoteName      = "text_note"
	planCardsName     = "plan_cards"
	dataTableName     = "data_table"
	barChartName      = "bar_chart"
	actionButtonsName = "action_buttons"
)

// catalogLeafNames is the band's membership and order in one place: the
// registry, the prompt fragments and the surface rules all read it.
var catalogLeafNames = []string{textNoteName, planCardsName, dataTableName, barChartName, actionButtonsName}

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
	if IsPanel(name) {
		return true
	}
	_, ok := renderers[name]
	return ok
}

// CatalogComponents lists the names a leg may lead with under the DEFAULT Ask
// surface set. Kept as a wrapper so existing callers (and the router's default
// path) read unchanged; new code should pass its surface set explicitly.
func CatalogComponents() []string {
	return CatalogComponentsFor(DefaultSurfacesAsk)
}

// CatalogComponentsFor lists the names a leg with this surface set may lead
// with: every leaf renderer plus the section container, minus the bands the
// set does not enable. Derived from the registry and the surface rules, not
// spelled out: a new component becomes routable by adding its one map entry,
// and a toggle moves a name in or out automatically.
//
// This is what keeps a router on-but-misconfigured from changing the prompt
// (a pick outside this set is ignored) AND what lets native panels become
// routable the moment a leg advertises them.
func CatalogComponentsFor(surfaces []Surface) []string {
	surfaces = NormalizeSurfaces(surfaces)
	names := make([]string, 0, len(renderers)+1)
	for name := range renderers {
		if !advertisedAs(name, surfaces) {
			continue
		}
		names = append(names, name)
	}
	for _, name := range panelNames() {
		if advertisedAs(name, surfaces) {
			names = append(names, name)
		}
	}
	names = append(names, sectionName)
	slices.Sort(names)
	return slices.Compact(names)
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

// renderChildren renders a container's nested directives, keeping the loop's
// error arm out of renderDepth (one flat function beats a nested if).
func renderChildren(children []Directive, depth int) ([]templ.Component, error) {
	comps := make([]templ.Component, 0, len(children))
	for _, child := range children {
		comp, err := renderDepth(child, depth+1)
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
		// decodeProps, not a hand-rolled decoder: it is the same routine every
		// leaf uses, so the container cannot accept props a leaf would reject
		// (this inline copy was the one place that could drift).
		title, err := decodeProps[SectionProps](d.Props)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(title.Title) == "" {
			return nil, errors.New("genui: section requires title")
		}
		children, err := renderChildren(d.Children, depth)
		if err != nil {
			return nil, err
		}
		return GenuiSection(title.Title, children), nil
	}
	// Installed panels render through their own provider, so no per-feature
	// entry lives in the renderer table. Checked before the generative
	// registry because a name can only be one or the other (the boot asserts
	// that a panel name is not also a leaf).
	if comp, isPanel, err := renderPanel(d.Component, d.Props); isPanel {
		return comp, err
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
