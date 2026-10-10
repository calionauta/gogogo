// SCOPE:layer=feature,removal=feature — chat render tests.
package chat_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/calionauta/gogogo/features/chat"
	"github.com/calionauta/gogogo/features/genui"
)

// TestChatPageWiring pins the page-level contracts: labeled composer,
// thinking hook, live region, stream opener, sounds loaded through the
// shared assets (zero custom sound JS — the global press cue and toast
// observer cover send/receive), and the empty-state guidance that
// teaches the feature on first run.
func TestChatPageWiring(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := chat.ChatIndex("a@b.c", "dev", "").Render(context.Background(), &buf); err != nil {
		t.Fatalf("ChatIndex: %v", err)
	}
	html := buf.String()
	for _, want := range []string{
		`<h1`,
		`id="chat-composer"`,
		`for="chat-prompt"`,
		`name="prompt"`,
		`contentType: &#39;form&#39;`,
		`id="chat-messages"`,
		`aria-live="polite"`,
		`id="chat-stream-opener"`,
		`/static/cuelume.js`,
		`data-cuelume`,
		`Ask anything — answers stream below`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("chat page missing %q", want)
		}
	}
	if strings.Contains(html, "play(") || strings.Contains(html, "AudioContext") {
		t.Errorf("chat page carries custom sound JS (global cues cover it)")
	}
}

// TestChatListStates pins the three list states, each rendered alone:
// empty guidance (never a blank column), thinking skeleton while the
// worker runs, error with a retry affordance.
func TestChatListStates(t *testing.T) {
	t.Parallel()
	render := func(msgs []chat.MessageView, thinking bool, errMsg string) string {
		t.Helper()
		var buf bytes.Buffer
		if err := chat.ChatList(msgs, thinking, errMsg).Render(context.Background(), &buf); err != nil {
			t.Fatalf("ChatList: %v", err)
		}
		return buf.String()
	}
	empty := render(nil, false, "")
	for _, want := range []string{`id="chat-messages"`, "Ask anything"} {
		if !strings.Contains(empty, want) {
			t.Errorf("empty list missing %q", want)
		}
	}
	thinking := render(nil, true, "")
	for _, want := range []string{"Thinking", "skeleton", `role="status"`} {
		if !strings.Contains(thinking, want) {
			t.Errorf("thinking state missing %q", want)
		}
	}
	failed := render(nil, false, "model unavailable")
	for _, want := range []string{"model unavailable", `role="alert"`, "Try again"} {
		if !strings.Contains(failed, want) {
			t.Errorf("error state missing %q", want)
		}
	}
}

// TestChatListRendersBubbles proves user/assistant messages render with
// distinct alignment and that component directives render inside the
// assistant bubble (generative UI inside chat, not beside it).
func TestChatListRendersBubbles(t *testing.T) {
	t.Parallel()
	comp, err := genui.Render(genui.Directive{
		Component: "text_note",
		Props:     map[string]any{"text": "nested hi"},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	msgs := []chat.MessageView{
		{Role: "user", Text: "plan my day"},
		{Role: "assistant", Text: "", Components: []templ.Component{comp}},
	}
	var buf bytes.Buffer
	if err := chat.ChatList(msgs, false, "").Render(context.Background(), &buf); err != nil {
		t.Fatalf("ChatList(bubbles): %v", err)
	}
	for _, want := range []string{"plan my day", "nested hi", "chat-end", "chat-start"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("bubbles missing %q", want)
		}
	}
}
