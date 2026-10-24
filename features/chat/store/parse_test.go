// SCOPE:feature — chat answer parsing tests.
package store

import (
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/genui/components"
)

// TestParseChatAnswerPrefersDirectives pins the hybrid contract: a model
// answer carrying catalog components renders as components (the
// generative-UI half); anything else degrades to a prose bubble — chat
// NEVER dies on weird output (a conversation that errors per message is
// unusable, so degradation is the contract here).
func TestParseChatAnswerPrefersDirectives(t *testing.T) {
	t.Parallel()
	dirs, dropped, fallback, err := ParseChatAnswer(`{"components":[{"type":"text_note","props":{"text":"hi"}}]}`)
	if err != nil {
		t.Fatalf("ParseChatAnswer(envelope): %v", err)
	}
	if dropped != 0 {
		t.Fatalf("ParseChatAnswer(envelope) dropped %d, want 0", dropped)
	}
	if fallback {
		t.Fatal("ParseChatAnswer(envelope) fell back to prose, want directives")
	}
	if len(dirs) != 1 || dirs[0].Component != "text_note" {
		t.Fatalf("parsed = %+v, want 1 text_note", dirs)
	}
}

// TestParseChatAnswerAcceptsInventedVisual pins the chat-only affordance:
// an html_block envelope parses to directives (advertised through the
// custom surface; chat renders it sanitized). The raw-HTML half is covered
// in the catalog's
// sanitize tests; here the contract is only that the envelope flows.
func TestParseChatAnswerAcceptsInventedVisual(t *testing.T) {
	t.Parallel()
	envelope := `{"components":[{"type":"html_block",` +
		`"props":{"html":"<div class=\"stats\"><p>done</p></div>"}}]}`
	dirs, dropped, fallback, err := ParseChatAnswer(envelope)
	if err != nil {
		t.Fatalf("ParseChatAnswer(html_block): %v", err)
	}
	if dropped != 0 {
		t.Fatalf("ParseChatAnswer(html_block) dropped %d, want 0", dropped)
	}
	if fallback {
		t.Fatal("ParseChatAnswer(html_block) fell back to prose, want directives")
	}
	if len(dirs) != 1 || dirs[0].Component != "html_block" {
		t.Fatalf("parsed = %+v, want 1 html_block", dirs)
	}
}

// livePlanAnswer is a model answer pasted from a live /chat run: one
// envelope whose sections park their children inside "props" under the
// envelope key, followed by a bar chart and action buttons. Valid JSON,
// rejected by strict prop decoding — so it took the prose path and the
// bubble showed this literal text ("mostra texto json"). Indentation is
// ours; the shape is the model's.
const livePlanAnswer = `{"components":[
  {"type":"section","props":{"title":"Today's Plan","components":[
    {"type":"section","props":{"title":"Morning","components":[
      {"type":"plan_cards","props":{"plans":[
        {"title":"Breakfast","detail":"7:30 AM – 8:00 AM – oatmeal & coffee"},
        {"title":"Team Meeting","detail":"9:00 AM – 10:00 AM – project updates"}]}}]}},
    {"type":"section","props":{"title":"Midday","components":[
      {"type":"plan_cards","props":{"plans":[
        {"title":"Client Call","detail":"12:00 PM – 1:00 PM"},
        {"title":"Lunch Break","detail":"1:00 PM – 1:30 PM"}]}}]}},
    {"type":"section","props":{"title":"Afternoon","components":[
      {"type":"plan_cards","props":{"plans":[
        {"title":"Code Review","detail":"2:00 PM – 3:30 PM"},
        {"title":"Write Docs","detail":"3:30 PM – 4:30 PM"}]}}]}},
    {"type":"section","props":{"title":"Evening","components":[
      {"type":"plan_cards","props":{"plans":[
        {"title":"Gym","detail":"6:00 PM – 7:00 PM"},
        {"title":"Dinner with Friends","detail":"7:30 PM – 9:00 PM"},
        {"title":"Read Book","detail":"9:30 PM – 10:30 PM"}]}}]}}]}},
  {"type":"bar_chart","props":{"title":"Time Allocation","bars":[
    {"label":"Morning","value":1.5},{"label":"Midday","value":2},
    {"label":"Afternoon","value":2},{"label":"Evening","value":4.5}]}},
  {"type":"action_buttons","props":{"actions":[
    {"label":"Add to Calendar","method":"POST","url":"/addToCalendar","fields":{}},
    {"label":"Set Reminder","method":"POST","url":"/setReminder","fields":{}}]}}
]}`

// TestParseChatAnswerRendersLivePlanAnswer is the regression for the
// reported symptom ("chat nao renderiza: mostra texto json"): the live
// answer above must render as components, nested sections included.
func TestParseChatAnswerRendersLivePlanAnswer(t *testing.T) {
	t.Parallel()
	dirs, dropped, fallback, err := ParseChatAnswer(livePlanAnswer)
	if err != nil {
		t.Fatalf("ParseChatAnswer(live answer): %v", err)
	}
	if dropped != 0 {
		t.Fatalf("ParseChatAnswer(live answer) dropped %d, want 0", dropped)
	}
	if fallback {
		t.Fatal("live answer rendered as prose (a wall of JSON in the bubble)")
	}
	if len(dirs) != 3 {
		t.Fatalf("top-level directives = %d, want section + bar_chart + action_buttons", len(dirs))
	}
	if dirs[0].Component != "section" || len(dirs[0].Children) != 4 {
		t.Fatalf("first directive = %+v, want a section with 4 child sections", dirs[0])
	}
	for i, day := range dirs[0].Children {
		if day.Component != "section" || len(day.Children) != 1 || day.Children[0].Component != "plan_cards" {
			t.Fatalf("child %d = %+v, want a section holding one plan_cards", i, day)
		}
	}
	if dirs[1].Component != "bar_chart" || dirs[2].Component != "action_buttons" {
		t.Fatalf("siblings = %s, %s, want bar_chart + action_buttons", dirs[1].Component, dirs[2].Component)
	}
	if _, err := components.RenderAll(dirs); err != nil {
		t.Fatalf("RenderAll(live answer): %v", err)
	}
}

// TestParseChatAnswerRendersMalformedModelOutput pins the other production
// shape this fixes: the envelope key repeated where an array element
// belongs (not JSON at all, the shape that used to answer with "could not
// shape the answer"). The section keeps its table and the second group
// joins it as a sibling — it used to hit the prose path instead.
func TestParseChatAnswerRendersMalformedModelOutput(t *testing.T) {
	t.Parallel()
	dirs, dropped, fallback, err := ParseChatAnswer(`{"components":[` +
		`{"type":"section","props":{"title":"My Day Plan"},"components":[` +
		`{"type":"data_table","props":{"headers":["Time"],"rows":[["08:00 AM"]]}}]},` +
		`"components":[{"type":"action_buttons","props":{"actions":[` +
		`{"label":"Add Activity","method":"POST","url":"/add-activity"}]}}]]}`)
	if err != nil {
		t.Fatalf("ParseChatAnswer(repeated envelope key): %v", err)
	}
	if dropped != 0 {
		t.Fatalf("ParseChatAnswer(repeated envelope key) dropped %d, want 0", dropped)
	}
	if fallback {
		t.Fatal("repeated envelope key answered with raw JSON in a prose bubble")
	}
	if len(dirs) != 2 || dirs[0].Component != "section" || len(dirs[0].Children) != 1 ||
		dirs[1].Component != "action_buttons" {
		t.Fatalf("parsed = %+v, want section(table) + sibling action_buttons", dirs)
	}
}

// TestParseChatAnswerKeepsFencedAnswerWithProse pins the other half of the
// live report: the model introduced the envelope with a line of prose and a
// ```json fence (the paste showed the fence inside the bubble). The prose
// is a preamble, the payload is the answer, so the payload wins.
func TestParseChatAnswerKeepsFencedAnswerWithProse(t *testing.T) {
	t.Parallel()
	dirs, dropped, fallback, err := ParseChatAnswer("Note\n```json\n" + livePlanAnswer + "\n```")
	if err != nil {
		t.Fatalf("ParseChatAnswer(fenced live answer): %v", err)
	}
	if dropped != 0 {
		t.Fatalf("ParseChatAnswer(fenced live answer) dropped %d, want 0", dropped)
	}
	if fallback {
		t.Fatal("fenced live answer rendered as prose")
	}
	if len(dirs) != 3 || dirs[0].Component != "section" {
		t.Fatalf("parsed = %+v, want the three directives read from the fence", dirs)
	}
}

// TestParseChatAnswerFallsBackToProse proves the degradation half:
// plain prose, undecodable props, and garbage all become one safe
// prose bubble (HTML-escaped at render, never raw). Unknown component
// names are NOT here: they render through the catch-all placeholder
// (see TestParseChatAnswerCatchAllUnknownType).
func TestParseChatAnswerFallsBackToProse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		raw         string
		wantDropped int
	}{
		{"just some words", 0},
		{`{"components":[{"type":"plan_cards","props":{"plans":"nope"}}]}`, 1},
		{"<script>alert(1)</script>", 0},
		{"", 0},
	} {
		dirs, dropped, fallback, err := ParseChatAnswer(tc.raw)
		if err != nil {
			t.Fatalf("ParseChatAnswer(%q) errored: %v (chat must degrade, not fail)", tc.raw, err)
		}
		if dropped != tc.wantDropped {
			t.Fatalf("ParseChatAnswer(%q) dropped %d, want %d", tc.raw, dropped, tc.wantDropped)
		}
		if !fallback || len(dirs) != 1 || dirs[0].Component != "text_note" {
			t.Fatalf("ParseChatAnswer(%q) = %+v fallback=%v, want 1 text_note fallback", tc.raw, dirs, fallback)
		}
	}
}

// TestParseChatAnswerCatchAllUnknownType pins the useDefaultRenderTool
// contract: an unknown component name renders as a named text_note
// placeholder instead of failing the whole answer — and it never leaks
// the payload. Red-proof: reject unknown types and the placeholder
// assertion fails while the bubble goes back to prose.
func TestParseChatAnswerCatchAllUnknownType(t *testing.T) {
	t.Parallel()
	dirs, dropped, fallback, err := ParseChatAnswer(`{"components":[{"type":"evil","props":{}}]}`)
	if err != nil {
		t.Fatalf("ParseChatAnswer(unknown type): %v", err)
	}
	if fallback {
		t.Fatal("unknown type degraded to prose, want the catch-all placeholder")
	}
	if dropped != 0 {
		t.Fatalf("catch-all is rendered, not dropped: dropped = %d, want 0", dropped)
	}
	if len(dirs) != 1 || dirs[0].Component != "text_note" {
		t.Fatalf("parsed = %+v, want 1 text_note placeholder", dirs)
	}
	text, _ := dirs[0].Props["text"].(string)
	if !strings.Contains(text, "evil") {
		t.Fatalf("placeholder %q does not name the requested type", text)
	}
	if strings.ContainsAny(text, "{}") {
		t.Fatalf("placeholder leaks the payload: %q", text)
	}
}

// TestParseChatAnswerRendersPartialEnvelope pins per-directive partial
// rendering: the valid component renders, the undecodable one is dropped
// and counted, and the model's prose around the envelope survives.
// Red-proof: fail the whole answer on one bad component and the valid
// sibling never renders.
func TestParseChatAnswerRendersPartialEnvelope(t *testing.T) {
	t.Parallel()
	raw := "Here is what I found:\n" +
		`{"components":[{"type":"text_note","props":{"text":"a good card"}},` +
		`{"type":"plan_cards","props":{"plans":"nope"}}]}`
	dirs, dropped, fallback, err := ParseChatAnswer(raw)
	if err != nil {
		t.Fatalf("ParseChatAnswer(partial): %v", err)
	}
	if fallback {
		t.Fatal("partial envelope degraded to prose, want the valid components")
	}
	if dropped != 1 {
		t.Fatalf("dropped = %d, want 1 (the bad plan_cards)", dropped)
	}
	var sawProse, sawCard, sawCatchAll bool
	for _, d := range dirs {
		text, _ := d.Props["text"].(string)
		switch {
		case d.Component == "text_note" && strings.Contains(text, "Here is what I found"):
			sawProse = true
		case d.Component == "text_note" && strings.Contains(text, "a good card"):
			sawCard = true
		case d.Component == "diagram":
			sawCatchAll = true
		}
	}
	if !sawProse || !sawCard {
		t.Fatalf("parsed = %+v, want the prose plus the good card", dirs)
	}
	if sawCatchAll {
		t.Fatalf("parsed = %+v, want no diagram here", dirs)
	}
	if _, err := ParseChatAnswerForRepair(raw); err == nil {
		t.Fatal("partial failure was accepted, want it sent back for repair")
	} else if !strings.Contains(err.Error(), "plan_cards") {
		t.Fatalf("repair error %q does not name the failed component", err)
	}
}

// TestParseChatAnswerNeverShowsThePayload is the reading contract of the
// bubble: the model's prose survives, the payload it failed on does not.
// A bubble of unreadable JSON is what a live run showed. Unknown types take
// the catch-all placeholder (never the raw envelope); undecodable props take
// the prose fallback.
func TestParseChatAnswerNeverShowsThePayload(t *testing.T) {
	t.Parallel()
	goodPayload := `{"components":[{"type":"plan_cards","props":{"plans":"nope"}}]}`
	t.Run("prose survives", func(t *testing.T) {
		t.Parallel()
		for _, raw := range []string{
			"Here is the plan as JSON:\n```json\n" + goodPayload + "\n```",
			"Here is the plan:\n" + goodPayload,
			"```json\n" + goodPayload + "\n```\nHope that helps!",
		} {
			dirs, _, fallback, err := ParseChatAnswer(raw)
			if err != nil {
				t.Fatalf("ParseChatAnswer(%q): %v", raw, err)
			}
			if !fallback {
				t.Fatalf("ParseChatAnswer(%q) did not degrade", raw)
			}
			text, _ := dirs[0].Props["text"].(string)
			if strings.ContainsAny(text, "{}") || strings.Contains(text, "components") {
				t.Fatalf("the bubble shows the payload: %q", text)
			}
			if text == "" {
				t.Fatalf("ParseChatAnswer(%q): the model's prose was dropped", raw)
			}
		}
	})
	t.Run("payload only leaves a readable line", func(t *testing.T) {
		t.Parallel()
		dirs, _, fallback, err := ParseChatAnswer(goodPayload)
		if err != nil {
			t.Fatalf("ParseChatAnswer(payload only): %v", err)
		}
		if !fallback {
			t.Fatal("payload only did not degrade")
		}
		if got, want := dirs[0].Props["text"], unrenderable; got != want {
			t.Fatalf("bubble text = %v, want %q", got, want)
		}
	})
	t.Run("unknown type never shows the envelope", func(t *testing.T) {
		t.Parallel()
		dirs, _, fallback, err := ParseChatAnswer(`{"components":[{"type":"evil","props":{"x":[1,2]}}]}`)
		if err != nil {
			t.Fatalf("ParseChatAnswer(unknown): %v", err)
		}
		if fallback {
			t.Fatal("unknown type degraded instead of using the catch-all")
		}
		text, _ := dirs[0].Props["text"].(string)
		if strings.ContainsAny(text, "{}[]") || strings.Contains(text, "components") {
			t.Fatalf("the placeholder shows the payload: %q", text)
		}
	})
}

// TestParseChatAnswerCapsProse bounds one giant bubble: model prose past
// the cap truncates with an ellipsis instead of breaking layout.
func TestParseChatAnswerCapsProse(t *testing.T) {
	t.Parallel()
	dirs, dropped, fallback, err := ParseChatAnswer(strings.Repeat("x", 5000))
	if dropped != 0 {
		t.Fatalf("ParseChatAnswer(long) dropped %d, want 0", dropped)
	}
	if err != nil {
		t.Fatalf("ParseChatAnswer(long): %v", err)
	}
	if !fallback {
		t.Fatal("long prose must take the fallback path")
	}
	text, ok := dirs[0].Props["text"].(string)
	if !ok {
		t.Fatal("fallback Props[\"text\"] is not a string")
	}
	if len(text) > 2100 || !strings.HasSuffix(text, "…") {
		t.Fatalf("prose not capped with ellipsis, len=%d", len(text))
	}
}

// TestChatRepairPolicySparesProse is the gate that keeps the repair loop
// out of chat's legitimate answers. Chat supports two shapes, and prose is
// one of them: asking the model to say it again as JSON costs a
// round-trip and makes the bubble worse. Red-proof: drop the
// isUnrenderable gate and a prose answer is reported as a failure again.
func TestChatRepairPolicySparesProse(t *testing.T) {
	t.Parallel()
	dirs, err := ParseChatAnswerForRepair("Here is my thinking, in prose alone.")
	if err != nil {
		t.Fatalf("prose answer reported as a failure worth repairing: %v", err)
	}
	if len(dirs) != 1 || dirs[0].Component != "text_note" {
		t.Fatalf("dirs = %+v, want the prose bubble", dirs)
	}
}

// TestChatRepairPolicyFlagsABarePayload is the other half: a payload that
// failed validation with no prose around it is exactly what a repair can
// fix, so it must be reported (and the final answer then degrades through
// ParseChatAnswer, which keeps the unrenderable placeholder).
func TestChatRepairPolicyFlagsABarePayload(t *testing.T) {
	t.Parallel()
	if _, err := ParseChatAnswerForRepair(`{"components":[{"type":"plan_cards","props":{"plans":"nope"}}]}`); err == nil {
		t.Fatal("a bare unvalidateable payload was accepted, want it sent back for repair")
	}
	// An unknown type alone is NOT a repair case: the catch-all placeholder
	// renders it, so there is nothing to fix.
	if _, err := ParseChatAnswerForRepair(`{"components":[{"type":"nope"}]}`); err != nil {
		t.Fatalf("unknown type alone was sent back for repair: %v", err)
	}
	// Prose beside the payload still counts as an answer to keep.
	if _, err := ParseChatAnswerForRepair("I could not build that.\n```json\n{\"nope\":1}\n```"); err != nil {
		t.Errorf("prose beside a payload was reported as a failure: %v", err)
	}
}

// TestParseChatAnswerRendersMermaidFences pins the chat mermaid affordance:
// a ```mermaid fence anywhere becomes a diagram directive even when the
// envelope failed, so the chat renders diagrams without requiring the model
// to emit the strict JSON shape. Red-proof: drop the fence scan and this
// input degrades to a prose bubble carrying raw fence text.
func TestParseChatAnswerRendersMermaidFences(t *testing.T) {
	t.Parallel()
	raw := "Here is the flow:\n```mermaid\nflowchart TD\n A-->B\n```\nHope it helps."
	dirs, dropped, _, err := ParseChatAnswer(raw)
	if err != nil {
		t.Fatalf("ParseChatAnswer(mermaid) = %v, want nil", err)
	}
	if dropped != 0 {
		t.Fatalf("ParseChatAnswer(mermaid) dropped %d, want 0", dropped)
	}
	var sawText, sawDiagram bool
	for _, d := range dirs {
		switch d.Component {
		case "text_note":
			sawText = true
			if txt, _ := d.Props["text"].(string); txt == "" || len(txt) > proseCap {
				t.Fatalf("prose bubble invalid: %q", txt)
			}
		case "diagram":
			sawDiagram = true
			src, _ := d.Props["source"].(string)
			if src == "" || len(src) > 8000 {
				t.Fatalf("diagram source invalid: %q", src)
			}
		}
	}
	if !sawDiagram {
		t.Fatalf("parsed = %+v, want a diagram directive", dirs)
	}
	if !sawText {
		t.Fatalf("parsed = %+v, want the surrounding prose kept", dirs)
	}
}
