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

// instanceID carries the server-assigned instance id through decodeProps.
//
// Embedded rather than added to each props struct: decodeProps is strict
// (DisallowUnknownFields), and the id is on EVERY leaf's props, so a repeated
// field would be ten places to forget. The field is unexported to the model by
// construction — parse.go deletes the key before validation, so nothing a model
// sends can reach it.
type instanceID struct {
	//nolint:tagliatelle // "_id" is the WIRE name and is deliberately not
	// camelCase: the leading underscore marks the key as server-owned, which is
	// the whole point of the field (see instance.go). Renaming it to "id" would
	// make it look like a model-settable prop.
	ID string `json:"_id"`
}

// TextNoteProps is a single prose card.
type TextNoteProps struct {
	instanceID
	Text string `json:"text"`
}

// SectionProps titles a group of nested components.
type SectionProps struct {
	instanceID
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
	instanceID
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
	instanceID
	Actions []Action `json:"actions"`
}

// HTMLBlockProps is model-composed visual markup for answers the fixed
// catalog cannot express (a timeline, a comparison, a dashboard invented
// for this request). The HTML is sanitized at parse time — the model
// styles with Tailwind/DaisyUI classes, never scripts, styles, handlers
// or media. Chat advertises it through the custom surface; a catalog-only
// surface set never does.
type HTMLBlockProps struct {
	instanceID
	HTML string `json:"html"`
}

// Plan is one actionable plan card.
//
// Action is OPTIONAL and is the only thing here a model uses to ask for a
// write. It exists because the alternative — a hardcoded
// `@post('/api/todos', ...)` inside the catalog's markup — put one feature's
// endpoint inside a shared leaf: genui would then know what a todo is (the
// cross-feature import the flat-deps gate forbids), and the SAME component
// could not serve any other feature. As a validated prop the action is generic
// (any same-origin path, see checkAction), the feature's knowledge stays in the
// feature, and a plan with no action renders no button instead of a button
// aimed at a page the user is not on.
type Plan struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
	// Action, when present, renders a submit button on this card's footer.
	// Method+URL+Fields are the same contract action_buttons uses, so there is
	// one vocabulary for "do something in this app" rather than two shapes.
	Action *Action `json:"action,omitempty"`
}

// PlanCardsProps is a list of plan cards with apply buttons.
type PlanCardsProps struct {
	instanceID
	Plans []Plan `json:"plans"`
}

// DataTableProps is a small read-only table.
type DataTableProps struct {
	instanceID
	Headers []string   `json:"headers"`
	Rows    [][]string `json:"rows"`
}

// DiagramProps is a Mermaid diagram rendered client-side with Pele.
// Source is raw Mermaid syntax (flowchart, sequence, class, state, ER,
// gantt, pie, mindmap, ...). The model emits text only; the browser
// turns it into SVG. Server validates shape, never renders SVG.
type DiagramProps struct {
	instanceID
	Source string `json:"source"`
	Title  string `json:"title,omitempty"`
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
	diagramName       = "diagram"
)

// catalogLeafNames is the band's membership and order in one place: the
// registry, the prompt fragments and the surface rules all read it.
var catalogLeafNames = []string{
	textNoteName, planCardsName, dataTableName,
	barChartName, actionButtonsName, diagramName,
}

// renderer decodes untrusted props into a component or fails.
type renderer func(map[string]any) (templ.Component, error)

// sectionName is the one container component: it resolves like a leaf
// (see Registered) and renders its children instead of props.
const sectionName = "section"

// htmlBlockName is the invented-markup affordance, advertised only when
// the custom surface is enabled (see advertisedAs), so a catalog-only
// surface set must treat this name as out of catalog.
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
// tests follow from this map (see Registered, PromptForSurfaces).
var renderers = map[string]renderer{
	"text_note": func(props map[string]any) (templ.Component, error) {
		p, err := decodeProps[TextNoteProps](props)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.Text) == "" {
			return nil, errors.New("genui: text_note requires text")
		}
		return GenuiTextNote(p.ID, p.Text), nil
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
			// A plan's action is checked TWICE, and both are needed.
			//
			// checkAction is the shape gate (same-origin path, POST|GET): it is
			// what stops a card becoming a request-forgery button. That alone
			// would still let the model aim at any same-origin route that
			// happens to exist, so checkDeclaredAction narrows it to the actions
			// FEATURES DECLARED (actions.go) — the model chooses WHICH offered
			// action with what content, never which endpoint.
			if plan.Action != nil {
				if err := checkAction(*plan.Action); err != nil {
					return nil, fmt.Errorf("genui: plan_cards action: %w", err)
				}
				if err := checkDeclaredAction(*plan.Action); err != nil {
					return nil, fmt.Errorf("genui: plan_cards action: %w", err)
				}
			}
		}
		return GenuiPlanCards(p.ID, p.Plans), nil
	},
	"data_table": func(props map[string]any) (templ.Component, error) {
		p, err := decodeProps[DataTableProps](props)
		if err != nil {
			return nil, err
		}
		if len(p.Headers) == 0 || len(p.Rows) == 0 {
			return nil, errors.New("genui: data_table requires headers and rows")
		}
		return GenuiDataTable(p.ID, p.Headers, p.Rows), nil
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
		return GenuiBarChart(p.ID, p.Title, p.Bars, peak), nil
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
			// Same declared-action gate as plan_cards: the shape check alone
			// would allow any existing same-origin route, so without this a
			// standalone button row could call an endpoint no feature offered.
			if err := checkDeclaredAction(a); err != nil {
				return nil, err
			}
		}
		return GenuiActionButtons(p.ID, p.Actions), nil
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
		return GenuiHTMLBlock(p.ID, clean), nil
	},
	diagramName: func(props map[string]any) (templ.Component, error) {
		p, err := decodeProps[DiagramProps](props)
		if err != nil {
			return nil, err
		}
		src, err := CleanDiagramSource(p.Source)
		if err != nil {
			return nil, err
		}
		return GenuiDiagram(p.ID, p.Title, src), nil
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

// CatalogComponents lists the names Chat may lead with under its default
// surface set. Kept as a wrapper so the router's default path reads
// unchanged; new code should pass its surface set explicitly.
func CatalogComponents() []string {
	return CatalogComponentsFor(DefaultSurfacesChat)
}

// CatalogComponentsFor lists the names a leg with this surface set may lead
// with: every leaf renderer plus the section container, minus the bands the
// set does not enable. Derived from the registry and the surface rules, not
// spelled out: a new component becomes routable by adding its one map entry,
// and a toggle moves a name in or out automatically.
//
// An empty set (prose-only: every band off) offers nothing, not even the
// section container — nesting with no leaves to nest is noise.
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
	if HasSurfaces(surfaces, SurfaceCatalog) {
		names = append(names, sectionName)
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// FilterDirectives drops every directive (recursively) whose component is
// unknown or outside the enabled vocabulary, so an answer that reaches for
// a band the tab turned off degrades instead of rendering it. A section
// left with no surviving child is dropped too — a titled box around nothing
// is the half-render the catalog refuses. Aliases resolve to their panel
// before the check, so a model spelling still filters as its panel. Nil in,
// nil out; empty in, empty out.
func FilterDirectives(dirs []Directive, surfaces []Surface) []Directive {
	surfaces = NormalizeSurfaces(surfaces)
	kept := make([]Directive, 0, len(dirs))
	for _, d := range dirs {
		if d.Component == sectionName {
			if !HasSurfaces(surfaces, SurfaceCatalog) {
				continue
			}
			children := FilterDirectives(d.Children, surfaces)
			if len(children) == 0 {
				continue
			}
			d.Children = children
			kept = append(kept, d)
			continue
		}
		canonical := ResolvePanelName(d.Component)
		if !Registered(canonical) || !advertisedAs(canonical, surfaces) {
			continue
		}
		d.Children = FilterDirectives(d.Children, surfaces)
		kept = append(kept, d)
	}
	return kept
}
