// SCOPE:layer=feature,removal=feature — gogen-ui component catalog.
package genui

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/a-h/templ"
)

// Directive is one model-emitted component: a registry name plus its
// props. The model never emits markup — only names from Registered()
// with props that decode into the structs below. Anything else fails
// closed at ParseDirectives, before any renderer runs.
type Directive struct {
	Component string
	Props     map[string]any
}

// TextNoteProps is a single prose card.
type TextNoteProps struct {
	Text string `json:"text"`
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
}

// Registered reports whether name is a catalog component.
func Registered(name string) bool {
	_, ok := renderers[name]
	return ok
}

// Render resolves one validated directive to its component. Unknown
// names fail even if ParseDirectives was skipped — defense in depth,
// same fail-closed shape as RequireOwner behind every handler.
func Render(d Directive) (templ.Component, error) {
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
		"data_table {headers:[], rows:[[]]}. " +
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

// ParseDirectives extracts and validates the component list from raw
// model output. Tolerant of fences and surrounding prose, strict about
// the catalog: one unknown type or undecodable props rejects the whole
// answer (a half-rendered generative region is worse than an error
// toast with retry).
func ParseDirectives(raw string) ([]Directive, error) {
	s := stripFences(strings.TrimSpace(raw))
	start, end := strings.IndexByte(s, '{'), strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return nil, errors.New("genui: no JSON object found")
	}
	var envelope struct {
		Components []struct {
			Type  string         `json:"type"`
			Props map[string]any `json:"props"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(s[start:end+1]), &envelope); err != nil {
		return nil, fmt.Errorf("genui: decode directives: %w", err)
	}
	if len(envelope.Components) == 0 {
		return nil, errors.New("genui: no components in answer")
	}
	dirs := make([]Directive, 0, len(envelope.Components))
	for _, c := range envelope.Components {
		if !Registered(c.Type) {
			return nil, fmt.Errorf("genui: unregistered component %q", c.Type)
		}
		if _, err := Render(Directive{Component: c.Type, Props: c.Props}); err != nil {
			return nil, err
		}
		dirs = append(dirs, Directive{Component: c.Type, Props: c.Props})
	}
	return dirs, nil
}
