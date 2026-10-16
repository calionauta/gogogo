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

// TestParseRepairsRepeatedEnvelopeKey pins the malformation that took Ask
// down in production (`genui: decode directives: invalid character ':'
// after array element`) and silently degraded chat to a raw-prose bubble.
// The model writes the envelope wrapper twice, the second time where an
// ARRAY ELEMENT belongs:
//
//	{"components":[{section…},"components":[{action_buttons…}]]}
//
// That is not JSON at all, yet the intent — two sibling components — is
// unambiguous, so the stray label and its value splice into the list they
// were meant to extend. The same output also nests a section's children
// under "components" instead of "children": the section must keep its
// table, not silently lose it.
// Red-proof: remove either half and this fails (parse error / empty
// section children).
func TestParseRepairsRepeatedEnvelopeKey(t *testing.T) {
	t.Parallel()
	raw := `{"components":[` +
		`{"type":"section","props":{"title":"My Day Plan"},"components":[` +
		`{"type":"data_table","props":{"headers":["Time","Activity"],` +
		`"rows":[["08:00 AM","Breakfast"]]}}]},` +
		`"components":[{"type":"action_buttons","props":{"actions":[` +
		`{"label":"Add Activity","method":"POST","url":"/add-activity"}]}}]]}`
	dirs, err := ParseDirectives(raw)
	if err != nil {
		t.Fatalf("ParseDirectives(repeated envelope key): %v", err)
	}
	if len(dirs) != 2 {
		t.Fatalf("parsed = %+v, want 2 top-level directives", dirs)
	}
	if dirs[0].Component != "section" || dirs[1].Component != "action_buttons" {
		t.Fatalf("parsed components = %s, %s; want section, action_buttons",
			dirs[0].Component, dirs[1].Component)
	}
	if len(dirs[0].Children) != 1 || dirs[0].Children[0].Component != "data_table" {
		t.Fatalf("section children = %+v, want 1 data_table", dirs[0].Children)
	}
}

// TestParseLeavesValidJSONAlone pins the strict path: a well-formed answer
// is decoded by encoding/json, so nothing tolerant can bend it — including
// an array of strings (a data_table header), which must survive intact
// rather than look like anything worth splicing.
func TestParseLeavesValidJSONAlone(t *testing.T) {
	t.Parallel()
	raw := `{"components":[{"type":"data_table","props":` +
		`{"headers":["Time","Activity"],"rows":[["08:00 AM","Breakfast"]]}}]}`
	dirs, err := ParseDirectives(raw)
	if err != nil {
		t.Fatalf("ParseDirectives(valid): %v", err)
	}
	headers, _ := dirs[0].Props["headers"].([]any)
	if len(headers) != 2 || headers[0] != "Time" || headers[1] != "Activity" {
		t.Fatalf("headers = %#v, want [Time Activity] untouched", dirs[0].Props["headers"])
	}
}

// TestParseChildrenInsideProps covers the shape the chat demo received: the
// model parks a section's children inside "props" under the envelope key it
// just read ("components"). That is valid JSON but fails strict prop
// decoding, so the whole answer was rejected and the conversation displayed
// the raw JSON in a prose bubble. Several nested sections, as in the report.
func TestParseChildrenInsideProps(t *testing.T) {
	t.Parallel()
	section := func(title, child string) string {
		return `{"type":"section","props":{"title":"` + title +
			`","components":[{"type":"plan_cards","props":{"plans":[{"title":"` + child +
			`","detail":"x"}]}}]}}`
	}
	raw := `{"components":[` + section("Morning", "Breakfast") + `,` +
		section("Midday", "Lunch") + `,` + section("Evening", "Gym") + `]}`
	dirs, err := ParseDirectives(raw)
	if err != nil {
		t.Fatalf("ParseDirectives(children in props): %v", err)
	}
	if len(dirs) != 3 {
		t.Fatalf("parsed = %+v, want 3 sections", dirs)
	}
	for i, want := range []string{"Morning", "Midday", "Evening"} {
		if dirs[i].Component != "section" || dirs[i].Props["title"] != want {
			t.Errorf("dirs[%d] = %+v, want section %q", i, dirs[i], want)
		}
		if len(dirs[i].Children) != 1 || dirs[i].Children[0].Component != "plan_cards" {
			t.Errorf("dirs[%d] children = %+v, want 1 plan_cards", i, dirs[i].Children)
		}
	}
}

// TestParseRepairsMismatchedCloser covers the other production error
// (`invalid character ']' after object key:value pair`): the model closes an
// object with the bracket it just used. A closer that does not match the
// open bracket still closes it; valid JSON is unaffected, since no valid
// document ever ends an object with ']'.
func TestParseRepairsMismatchedCloser(t *testing.T) {
	t.Parallel()
	raw := `{"components":[{"type":"section","props":{"title":"Today"],` +
		`"children":[{"type":"text_note","props":{"text":"hi"}}]}]}`
	dirs, err := ParseDirectives(raw)
	if err != nil {
		t.Fatalf("ParseDirectives(mismatched closer): %v", err)
	}
	if len(dirs) != 1 || dirs[0].Component != "section" || dirs[0].Props["title"] != "Today" {
		t.Fatalf("parsed = %+v, want 1 section titled Today", dirs)
	}
	if len(dirs[0].Children) != 1 || dirs[0].Children[0].Component != "text_note" {
		t.Fatalf("section children = %+v, want 1 text_note", dirs[0].Children)
	}
}

// TestParseRendersTruncatedAnswer: a model cut off at its token limit still
// gets the components that arrived on screen — the finished one whole, the
// half-written one whatever survived — instead of the whole answer landing
// as raw JSON in the transcript.
func TestParseRendersTruncatedAnswer(t *testing.T) {
	t.Parallel()
	raw := `{"components":[{"type":"text_note","props":{"text":"whole"}},` +
		`{"type":"plan_cards","props":{"plans":[{"title":"cut off her`
	dirs, err := ParseDirectives(raw)
	if err != nil {
		t.Fatalf("ParseDirectives(truncated): %v", err)
	}
	if len(dirs) == 0 || dirs[0].Component != "text_note" || dirs[0].Props["text"] != "whole" {
		t.Fatalf("parsed = %+v, want the intact text_note first", dirs)
	}
}

// TestParseSurvivesDeeplyNestedGarbage: the reader's depth cap is a stack
// guard, not a catalog rule — a wall of brackets must be rejected (or read
// partially) without exhausting the stack.
func TestParseSurvivesDeeplyNestedGarbage(t *testing.T) {
	t.Parallel()
	raw := `{"components":[` + strings.Repeat(`[`, 5000) +
		`{"type":"text_note","props":{"text":"deep"}}`
	if _, err := ParseDirectives(raw); err == nil {
		t.Fatal("ParseDirectives(deep brackets) = nil error, want rejection")
	}
}

// TestParseRepairsTrailingCommas pins model sloppiness that is not JSON at
// all but whose intent is unambiguous: a comma before } or ], so a small
// model's answer renders instead of costing a second billable call.
func TestParseRepairsTrailingCommas(t *testing.T) {
	t.Parallel()
	dirs, err := ParseDirectives(`{"components":[{"type":"text_note","props":{"text":"hi",},},]}`)
	if err != nil {
		t.Fatalf("ParseDirectives(trailing commas): %v", err)
	}
	if len(dirs) != 1 || dirs[0].Component != "text_note" {
		t.Fatalf("parsed = %+v, want 1 text_note", dirs)
	}
}
