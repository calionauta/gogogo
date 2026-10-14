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

// TestParseToleratesTrailingBrace pins the Ask failure report ("could
// not shape the answer (genui: decode directives: invalid character '}'
// after top-level value)"): real models overshoot brace balancing on
// props:{...} + array ] + outer } and emit one extra "}". The first
// object is the answer; trailing garbage must not fail it.
func TestParseToleratesTrailingBrace(t *testing.T) {
	t.Parallel()
	raw := `{"components":[{"type":"text_note","props":{"text":"hi"}}]} }`
	dirs, err := ParseDirectives(raw)
	if err != nil {
		t.Fatalf("ParseDirectives(trailing brace) = %v, want 1 directive", err)
	}
	if len(dirs) != 1 || dirs[0].Component != "text_note" {
		t.Fatalf("parsed = %+v, want 1 text_note", dirs)
	}
}

// TestParseToleratesDoubledObject pins the echo case: a model that
// repeats the envelope twice still yields the first answer instead of
// "invalid character '{' after top-level value".
func TestParseToleratesDoubledObject(t *testing.T) {
	t.Parallel()
	raw := `{"components":[{"type":"text_note","props":{"text":"hi"}}]}` +
		` {"components":[{"type":"text_note","props":{"text":"hi again"}}]}`
	dirs, err := ParseDirectives(raw)
	if err != nil {
		t.Fatalf("ParseDirectives(doubled) = %v, want 1 directive", err)
	}
	if len(dirs) != 1 || dirs[0].Component != "text_note" {
		t.Fatalf("parsed = %+v, want first object's text_note", dirs)
	}
}

// TestParseLiftsSectionPropsComponents pins the plan-my-day shape: the
// model nests the section's list inside props ("components") instead of
// the canonical "children". The list lifts into Children; the whole
// answer renders instead of degrading to a raw-JSON text bubble.
func TestParseLiftsSectionPropsComponents(t *testing.T) {
	t.Parallel()
	raw := `{"components":[{"type":"section","props":{"title":"Today's Plan",` +
		`"components":[{"type":"text_note","props":{"text":"hello"}}]}}]}`
	dirs, err := ParseDirectives(raw)
	if err != nil {
		t.Fatalf("ParseDirectives(props.components) = %v, want 1 section", err)
	}
	if len(dirs) != 1 || dirs[0].Component != "section" || len(dirs[0].Children) != 1 {
		t.Fatalf("parsed = %+v, want section with 1 lifted child", dirs)
	}
	if _, err := RenderAll(dirs); err != nil {
		t.Fatalf("RenderAll(lifted section): %v", err)
	}
}

// TestParseKeepsFencesInsideProps guards the fence-strip ordering rule:
// ``` inside a props string (a text_note showing a code sample) is
// content, not wrapping — stripping it mangles valid JSON into a
// fragment.
func TestParseKeepsFencesInsideProps(t *testing.T) {
	t.Parallel()
	raw := `{"components":[{"type":"text_note","props":{"text":"run ` + "```go" +
		`\nfmt.Println(1)\n` + "```" + ` after lunch"}}]}`
	dirs, err := ParseDirectives(raw)
	if err != nil {
		t.Fatalf("ParseDirectives(fences in props) = %v, want 1 directive", err)
	}
	if len(dirs) != 1 || dirs[0].Component != "text_note" {
		t.Fatalf("parsed = %+v, want 1 text_note", dirs)
	}
	text, _ := dirs[0].Props["text"].(string)
	if !strings.Contains(text, "```go") {
		t.Fatalf("props text lost its code fences: %q", text)
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
