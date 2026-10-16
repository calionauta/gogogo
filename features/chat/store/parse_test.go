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
// NEVER dies on weird output (unlike the Ask demo, which fails closed
// to showcase strictness; a conversation that errors per message is
// unusable, so degradation is the contract here).
func TestParseChatAnswerPrefersDirectives(t *testing.T) {
	t.Parallel()
	dirs, fallback, err := ParseChatAnswer(`{"components":[{"type":"text_note","props":{"text":"hi"}}]}`)
	if err != nil {
		t.Fatalf("ParseChatAnswer(envelope): %v", err)
	}
	if fallback {
		t.Fatal("ParseChatAnswer(envelope) fell back to prose, want directives")
	}
	if len(dirs) != 1 || dirs[0].Component != "text_note" {
		t.Fatalf("parsed = %+v, want 1 text_note", dirs)
	}
}

// TestParseChatAnswerAcceptsInventedVisual pins the chat-only affordance:
// an html_block envelope parses to directives (Ask never advertises it,
// chat renders it sanitized). The raw-HTML half is covered in genui's
// sanitize tests; here the contract is only that the envelope flows.
func TestParseChatAnswerAcceptsInventedVisual(t *testing.T) {
	t.Parallel()
	envelope := `{"components":[{"type":"html_block",` +
		`"props":{"html":"<div class=\"stats\"><p>done</p></div>"}}]}`
	dirs, fallback, err := ParseChatAnswer(envelope)
	if err != nil {
		t.Fatalf("ParseChatAnswer(html_block): %v", err)
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
	dirs, fallback, err := ParseChatAnswer(livePlanAnswer)
	if err != nil {
		t.Fatalf("ParseChatAnswer(live answer): %v", err)
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
// belongs (not JSON at all, the shape that made Ask answer with "could not
// shape the answer"). The section keeps its table and the second group
// joins it as a sibling — it used to hit the prose path instead.
func TestParseChatAnswerRendersMalformedModelOutput(t *testing.T) {
	t.Parallel()
	dirs, fallback, err := ParseChatAnswer(`{"components":[` +
		`{"type":"section","props":{"title":"My Day Plan"},"components":[` +
		`{"type":"data_table","props":{"headers":["Time"],"rows":[["08:00 AM"]]}}]},` +
		`"components":[{"type":"action_buttons","props":{"actions":[` +
		`{"label":"Add Activity","method":"POST","url":"/add-activity"}]}}]]}`)
	if err != nil {
		t.Fatalf("ParseChatAnswer(repeated envelope key): %v", err)
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
	dirs, fallback, err := ParseChatAnswer("Note\n```json\n" + livePlanAnswer + "\n```")
	if err != nil {
		t.Fatalf("ParseChatAnswer(fenced live answer): %v", err)
	}
	if fallback {
		t.Fatal("fenced live answer rendered as prose")
	}
	if len(dirs) != 3 || dirs[0].Component != "section" {
		t.Fatalf("parsed = %+v, want the three directives read from the fence", dirs)
	}
}

// TestParseChatAnswerFallsBackToProse proves the degradation half:
// plain prose, unknown components, and garbage all become one safe
// prose bubble (HTML-escaped at render, never raw).
func TestParseChatAnswerFallsBackToProse(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"just some words",
		`{"components":[{"type":"evil","props":{}}]}`,
		`{"components":[{"type":"plan_cards","props":{"plans":"nope"}}]}`,
		"<script>alert(1)</script>",
		"",
	} {
		dirs, fallback, err := ParseChatAnswer(raw)
		if err != nil {
			t.Fatalf("ParseChatAnswer(%q) errored: %v (chat must degrade, not fail)", raw, err)
		}
		if !fallback || len(dirs) != 1 || dirs[0].Component != "text_note" {
			t.Fatalf("ParseChatAnswer(%q) = %+v fallback=%v, want 1 text_note fallback", raw, dirs, fallback)
		}
	}
}

// TestParseChatAnswerNeverShowsThePayload is the reading contract of the
// fallback bubble: the model's prose survives, the payload it failed on
// does not. A bubble of unreadable JSON is what a live run showed.
func TestParseChatAnswerNeverShowsThePayload(t *testing.T) {
	t.Parallel()
	payload := `{"components":[{"type":"evil","props":{}}]}`
	t.Run("prose survives", func(t *testing.T) {
		t.Parallel()
		for _, raw := range []string{
			"Here is the plan as JSON:\n```json\n" + payload + "\n```",
			"Here is the plan:\n" + payload,
			"```json\n" + payload + "\n```\nHope that helps!",
		} {
			dirs, fallback, err := ParseChatAnswer(raw)
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
		dirs, fallback, err := ParseChatAnswer(payload)
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
}

// TestParseChatAnswerCapsProse bounds one giant bubble: model prose past
// the cap truncates with an ellipsis instead of breaking layout.
func TestParseChatAnswerCapsProse(t *testing.T) {
	t.Parallel()
	dirs, fallback, err := ParseChatAnswer(strings.Repeat("x", 5000))
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
