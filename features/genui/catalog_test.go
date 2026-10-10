// SCOPE:layer=feature,removal=feature — gogen-ui catalog tests.
package genui

import (
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
	for _, want := range []string{"text_note", "plan_cards", "data_table"} {
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
