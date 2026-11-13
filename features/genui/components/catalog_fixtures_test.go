// The catalog's fixture corpus: what each component accepts and what it must
// refuse, expressed as the JSON a model actually emits.
//
// WHY THIS FILE EXISTS. The OpenUI/A2UI comparison (docs/research/
// genui-schema-patterns-2026-11.md) settled on one practice worth copying: a
// component library is only trustworthy once every component carries worked
// examples AND counter-examples, because a prompt that describes a component and
// a validator that rejects it is the failure mode that reaches users. OpenUI's
// reliability note measured a wrong example costing double-digit completion
// points, and A2UI's catalogs ship their components as schema with
// `unevaluatedProperties: false` — the same rule `decodeProps` already applies.
//
// This is a CONTRACT harness, not a model benchmark. It measures what a
// generation of a given shape does (parses? renders?) with a stub, so it runs in
// CI with no provider and no key. Model reliability itself is measured outside
// the suite, against the operator's own catalog — the repo never calls a real
// model in a test.
package components

import (
	"slices"
	"testing"
)

// fixture is one generation shape and its verdict. valid means "parses AND
// renders"; invalid means the whole answer is refused (the catalog fails
// closed on purpose: a half-rendered region is worse than an error).
type fixture struct {
	name  string
	json  string
	valid bool
}

// catalogFixtures covers every catalog leaf plus the container. The invalid rows
// outnumber the valid ones by design: the catalog's job is refusal.
var catalogFixtures = []fixture{
	{
		name:  "text_note",
		json:  `{"components":[{"type":"text_note","props":{"text":"Good morning"}}]}`,
		valid: true,
	},
	{
		name:  "text_note empty text",
		json:  `{"components":[{"type":"text_note","props":{"text":"   "}}]}`,
		valid: false,
	},
	{
		name:  "text_note invents a prop",
		json:  `{"components":[{"type":"text_note","props":{"text":"x","tone":"warm"}}]}`,
		valid: false,
	},
	{
		name: "data_table",
		json: `{"components":[{"type":"data_table","props":` +
			`{"headers":["Item","Price"],"rows":[["Latte","4.30"]]}}]}`,
		valid: true,
	},
	{
		name:  "data_table without rows",
		json:  `{"components":[{"type":"data_table","props":{"headers":["Item"]}}]}`,
		valid: false,
	},
	{
		name: "bar_chart",
		json: `{"components":[{"type":"bar_chart","props":` +
			`{"title":"Orders","bars":[{"label":"mon","value":3}]}}]}`,
		valid: true,
	},
	{
		name: "bar_chart negative value",
		json: `{"components":[{"type":"bar_chart","props":` +
			`{"title":"Orders","bars":[{"label":"mon","value":-1}]}}]}`,
		valid: false,
	},
	{
		name: "bar_chart all zero",
		json: `{"components":[{"type":"bar_chart","props":` +
			`{"title":"Orders","bars":[{"label":"mon","value":0}]}}]}`,
		valid: false,
	},
	{
		name: "plan_cards",
		json: `{"components":[{"type":"plan_cards","props":{"plans":[` +
			`{"title":"Call the supplier","detail":"before 10am"}]}}]}`,
		valid: true,
	},
	{
		name: "plan_cards without a title",
		json: `{"components":[{"type":"plan_cards","props":{"plans":[` +
			`{"detail":"untitled"}]}}]}`,
		valid: false,
	},
	{
		name: "plan_cards with an undeclared action",
		json: `{"components":[{"type":"plan_cards","props":{"plans":[{"title":"x",` +
			`"action":{"label":"Go","method":"POST","url":"/api/invented","fields":{}}}]}}]}`,
		valid: false,
	},
	{
		name: "plan_cards with an absolute action",
		json: `{"components":[{"type":"plan_cards","props":{"plans":[{"title":"x",` +
			`"action":{"label":"Go","method":"POST","url":"https://evil.example/x","fields":{}}}]}}]}`,
		valid: false,
	},
	{
		name:  "action_buttons empty",
		json:  `{"components":[{"type":"action_buttons","props":{"actions":[]}}]}`,
		valid: false,
	},
	{
		name: "section holds a child",
		json: `{"components":[{"type":"section","props":{"title":"Today"},` +
			`"children":[{"type":"text_note","props":{"text":"two deliveries"}}]}]}`,
		valid: true,
	},
	{
		// A title with no children is ACCEPTED, deliberately: a section is a
		// heading, and an answer that groups nothing still renders its heading.
		// The fixture records the behavior rather than wishing it were stricter —
		// a corpus that disagrees with the code is the drift this file exists to
		// catch.
		name:  "section without children",
		json:  `{"components":[{"type":"section","props":{"title":"Today"}}]}`,
		valid: true,
	},
	{
		name:  "diagram without source",
		json:  `{"components":[{"type":"diagram","props":{"source":"  "}}]}`,
		valid: false,
	},
	{
		name:  "unknown component",
		json:  `{"components":[{"type":"kiosk_menu","props":{}}]}`,
		valid: false,
	},
	{
		name: "one bad component refuses the answer",
		json: `{"components":[{"type":"text_note","props":{"text":"ok"}},` +
			`{"type":"nope","props":{}}]}`,
		valid: false,
	},
}

// TestCatalogFixtures applies the corpus: a valid shape parses AND renders, an
// invalid one is refused, and the refusal is the whole answer.
func TestCatalogFixtures(t *testing.T) {
	for _, f := range catalogFixtures {
		t.Run(f.name, func(t *testing.T) {
			dirs, err := ParseDirectives(f.json)
			if !f.valid {
				if err == nil {
					t.Fatalf("want the answer refused, it parsed into %d directive(s)", len(dirs))
				}
				return
			}
			if err != nil {
				t.Fatalf("want a valid parse, got %v", err)
			}
			if _, rerr := RenderAll(dirs); rerr != nil {
				t.Fatalf("a valid fixture failed to render: %v", rerr)
			}
		})
	}
}

// TestCatalogPromptAndValidatorAgree is the parity gate the prompt cannot have on
// its own: every advertised name resolves to a renderer, and every advertised
// name is spelled in the prompt. A name in one and not the other is a model
// being told to emit something the parser refuses (or never told about something
// it may emit) — the drift a schema-as-data catalog exists to prevent.
func TestCatalogPromptAndValidatorAgree(t *testing.T) {
	catalog := NormalizeSurfaces([]Surface{SurfaceCatalog})
	advertised := CatalogComponentsFor(catalog)
	prompt := PromptForSurfaces(catalog)

	for _, name := range catalogLeafNames {
		if !slices.Contains(advertised, name) {
			t.Errorf("%q is in the leaf band but not advertised on the catalog surface", name)
		}
		if _, ok := renderers[name]; !ok {
			t.Errorf("%q is advertised with no renderer: the parser would refuse it", name)
		}
		if !containsWord(prompt, name) {
			t.Errorf("%q is advertised but never spelled in the prompt", name)
		}
	}
	for _, name := range advertised {
		if _, ok := renderers[name]; !ok && name != sectionName {
			t.Errorf("advertised %q has no renderer", name)
		}
		if !containsWord(prompt, name) {
			t.Errorf("advertised %q is missing from the prompt", name)
		}
	}
	// html_block is the custom surface's affordance; a catalog-only set must not
	// advertise it, or the model is asked for markup the surface cannot render.
	if slices.Contains(advertised, htmlBlockName) {
		t.Error("html_block leaked into the catalog-only vocabulary")
	}
}

// containsWord reports whether name appears in prompt as a whole token, so
// "section" is not satisfied by "subsection".
func containsWord(prompt, name string) bool {
	for i := 0; i+len(name) <= len(prompt); i++ {
		if prompt[i:i+len(name)] != name {
			continue
		}
		beforeOK := i == 0 || !isNameByte(prompt[i-1])
		afterOK := i+len(name) == len(prompt) || !isNameByte(prompt[i+len(name)])
		if beforeOK && afterOK {
			return true
		}
	}
	return false
}

func isNameByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
