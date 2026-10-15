// SCOPE:feature — gogen-ui catalog tests.
package components

import (
	"strconv"
	"strings"
	"testing"
)

// TestCatalogRejectsUnknownComponent is the fail-closed contract: a model
// emitting a component outside the registry must error, never render. An
// unknown directive reaching a renderer would be a silent empty region
// (or worse, the wrong component) — the exact drift the catalog exists
// to prevent. The props are deliberately valid for a real component, so
// the rejection must come from the type check itself, not from a
// coincidental props failure downstream.
func TestCatalogRejectsUnknownComponent(t *testing.T) {
	t.Parallel()
	dirs, err := ParseDirectives(`{"components":[{"type":"nope","props":{"text":"x"}}]}`)
	if err == nil {
		t.Fatal("ParseDirectives(unknown type) = nil error, want rejection")
	}
	if dirs != nil {
		t.Fatalf("ParseDirectives(unknown type) returned %+v, want nil", dirs)
	}
}

// TestParseToleratesFencesAndProse pins the tolerant front-door: models
// wrap JSON in fences and prose. The parser strips both (same philosophy
// as llm.parseStringArray) instead of 400ing on valid content.
func TestParseToleratesFencesAndProse(t *testing.T) {
	t.Parallel()
	raw := "Here you go:\n```json\n" +
		`{"components":[{"type":"text_note","props":{"text":"hi"}},` +
		`{"type":"plan_cards","props":{"plans":[{"title":"A","detail":"do it"}]}}]}` +
		"\n```\nHope that helps!"
	dirs, err := ParseDirectives(raw)
	if err != nil {
		t.Fatalf("ParseDirectives(fenced) = %v, want 2 directives", err)
	}
	if len(dirs) != 2 || dirs[0].Component != "text_note" || dirs[1].Component != "plan_cards" {
		t.Fatalf("parsed = %+v, want text_note + plan_cards", dirs)
	}
}

// TestCatalogListsRegistered pins the showcase catalog: every name the
// prompt advertises must resolve to a renderer, or the model is invited
// to emit components that fail closed at parse time.
func TestCatalogListsRegistered(t *testing.T) {
	t.Parallel()
	for _, want := range []string{"text_note", "plan_cards", "data_table", "bar_chart", "action_buttons", "section"} {
		if !Registered(want) {
			t.Errorf("catalog missing %q (prompt advertises it)", want)
		}
		if PromptForCatalog() == "" || !strings.Contains(PromptForCatalog(), want) {
			t.Errorf("prompt does not advertise %q", want)
		}
	}
}

// TestParseRejectsMalformedProps proves validation goes past the type
// name: a registered component with undecodable props must fail, not
// render a zero-value card.
func TestParseRejectsMalformedProps(t *testing.T) {
	t.Parallel()
	if _, err := ParseDirectives(`{"components":[{"type":"plan_cards","props":{"plans":"not-an-array"}}]}`); err == nil {
		t.Fatal("ParseDirectives(bad props) = nil error, want rejection")
	}
}

// TestActionButtonsStaySameOrigin pins the SSRF guard: an action
// posting off-origin (or to a non-relative URL) must fail closed, or a
// model could turn every Ask answer into a request forgery button.
func TestActionButtonsStaySameOrigin(t *testing.T) {
	t.Parallel()
	for _, url := range []string{
		"https://evil.example/x",
		"//evil.example/x",
		"javascript:alert(1)",
		"/api/todos with space",
	} {
		raw := `{"components":[{"type":"action_buttons","props":{` +
			`"actions":[{"label":"x","method":"POST","url":` + strconv.Quote(url) + `}]}}]}`
		if _, err := ParseDirectives(raw); err == nil {
			t.Fatalf("ParseDirectives(action %q) = nil error, want rejection", url)
		}
	}
}

// TestBarChartNeedsPositiveBars pins honest scaling: an empty or
// all-zero series renders nothing meaningful, so it fails instead of
// drawing a flat lie.
func TestBarChartNeedsPositiveBars(t *testing.T) {
	t.Parallel()
	if _, err := ParseDirectives(`{"components":[{"type":"bar_chart","props":{"title":"W","bars":[]}}]}`); err == nil {
		t.Fatal("ParseDirectives(empty bars) = nil error, want rejection")
	}
}

// TestSectionNestsComponents pins recursive composition: a section
// carries titled children that render inside it, so answers can group
// (week plan holding its table) instead of only stacking siblings.
func TestSectionNestsComponents(t *testing.T) {
	t.Parallel()
	dirs, err := ParseDirectives(`{"components":[` +
		`{"type":"section","props":{"title":"Week"},"children":[` +
		`{"type":"text_note","props":{"text":"nested hello"}}]}]}`)
	if err != nil {
		t.Fatalf("ParseDirectives(nested) = %v, want 1 section", err)
	}
	if len(dirs) != 1 || dirs[0].Component != "section" || len(dirs[0].Children) != 1 {
		t.Fatalf("parsed = %+v, want section with 1 child", dirs)
	}
	comps, err := RenderAll(dirs)
	if err != nil {
		t.Fatalf("RenderAll(nested): %v", err)
	}
	if len(comps) != 1 {
		t.Fatalf("rendered %d components, want 1 section", len(comps))
	}
}

// TestSectionRejectsUnknownChild proves the recursion validates at
// every level: an unknown nested type fails the whole answer, not just
// the top level (a renderer that skipped children validation would
// green this test forever).
func TestSectionRejectsUnknownChild(t *testing.T) {
	t.Parallel()
	if _, err := ParseDirectives(`{"components":[` +
		`{"type":"section","props":{"title":"W"},"children":[` +
		`{"type":"evil","props":{"text":"x"}}]}]}`); err == nil {
		t.Fatal("ParseDirectives(unknown nested) = nil error, want rejection")
	}
}

// TestSectionDepthCapped bounds hostile nesting: 5-deep sections must
// fail instead of recursing the worker's stack on attacker input.
func TestSectionDepthCapped(t *testing.T) {
	t.Parallel()
	deep := `{"type":"section","props":{"title":"d"}}`
	nested := deep
	for range 5 {
		nested = `{"type":"section","props":{"title":"d"},"children":[` + nested + `]}`
	}
	if _, err := ParseDirectives(`{"components":[` + nested + `]}`); err == nil {
		t.Fatal("ParseDirectives(5-deep) = nil error, want depth rejection")
	}
}

// TestParseBridgesChildrenInProps pins the tolerance that fixes real
// model output: children nested inside props (the envelope shape puts
// everything under "props") bridge to directive level, and the key is
// dropped so strict prop decoding still passes. Red-proof: remove the
// bridge and this fails with an unknown-field error.
func TestParseBridgesChildrenInProps(t *testing.T) {
	t.Parallel()
	raw := `{"components":[{"type":"section",` +
		`"props":{"title":"Today","children":[{"type":"plan_cards",` +
		`"props":{"plans":[{"title":"Jog","detail":"30 min"}]}}]}}]}`
	dirs, err := ParseDirectives(raw)
	if err != nil {
		t.Fatalf("ParseDirectives(children-in-props): %v", err)
	}
	if len(dirs) != 1 || dirs[0].Component != "section" {
		t.Fatalf("parsed = %+v, want 1 section", dirs)
	}
	if len(dirs[0].Children) != 1 || dirs[0].Children[0].Component != "plan_cards" {
		t.Fatalf("section children = %+v, want 1 plan_cards", dirs[0].Children)
	}
	if _, ok := dirs[0].Props["children"]; ok {
		t.Error("bridged children key must be dropped from props")
	}
}

// TestParseRepairsTrailingCommas pins the other half of model sloppiness:
// trailing commas before } and ] are stripped (outside string literals
// only), so small-model output parses without a second billable call.
// Valid JSON passes through untouched.
func TestParseRepairsTrailingCommas(t *testing.T) {
	t.Parallel()
	dirs, err := ParseDirectives(`{"components":[{"type":"text_note","props":{"text":"hi",},},]}`)
	if err != nil {
		t.Fatalf("ParseDirectives(trailing commas): %v", err)
	}
	if len(dirs) != 1 || dirs[0].Component != "text_note" {
		t.Fatalf("parsed = %+v, want 1 text_note", dirs)
	}
	if got := repairJSON(`{"a":"x, y","b":[1,]}`); got != `{"a":"x, y","b":[1]}` {
		t.Fatalf("repairJSON mangled literals: %q", got)
	}
}
