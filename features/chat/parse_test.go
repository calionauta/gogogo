// SCOPE:layer=feature,removal=feature — chat answer parsing tests.
package chat

import (
	"strings"
	"testing"
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
