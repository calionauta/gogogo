// SCOPE:feature — chat render tests.
package chat_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	html2 "golang.org/x/net/html"

	chatcomponents "github.com/calionauta/gogogo/features/chat/components"
	"github.com/calionauta/gogogo/features/chat/store"
	comp "github.com/calionauta/gogogo/features/genui/components"
)

// TestChatPageWiring pins the page-level contracts: labeled composer,
// thinking hook, live region, stream opener, sounds loaded through the
// shared assets (zero custom sound JS — the global press cue and toast
// observer cover send/receive), and the empty-state guidance that
// teaches the feature on first run.
func TestChatPageWiring(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := chatcomponents.ChatIndex(
		"a@b.c", "user", "dev", "", nil, false, true, true, true,
	).Render(context.Background(), &buf); err != nil {
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
		"/static/cuelume.js?v=",
		`data-cuelume`,
		`data-show="$chat_empty"`,
		`Ask your app anything`,
		`data-attr:disabled="!!($chat_thinking || !$chat_ready)"`,
		`$chat_working`,
		`form="chat-composer"`,
		`name="surfaces"`,
		`value="catalog"`,
		`value="custom"`,
		`value="native"`,
		`Thinking…`,
		`Connecting to the live stream`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("chat page missing %q", want)
		}
	}
	// Attribute values arrive entity-escaped; unescape before reading the
	// seeded signal object as JSON.
	seeded := html2.UnescapeString(html)
	if !strings.Contains(seeded, `"chat_empty":true`) {
		t.Errorf("page did not seed $chat_empty for an empty conversation")
	}
	if !strings.Contains(seeded, "chat_empty = false") {
		t.Errorf("composer never clears $chat_empty — the guidance would sit under every answer")
	}
	// `\.play(` is the HTMLMediaElement cue call. A bare "play(" matched any
	// identifier ending in those letters (postReplay, kickReplay), which is a
	// name collision rather than a sound implementation.
	if strings.Contains(html, ".play(") || strings.Contains(html, "AudioContext") {
		t.Errorf("chat page carries custom sound JS (global cues cover it)")
	}
	// The spinner reads as the last message: appends into #chat-messages
	// land before it in DOM order, so the user's echo never sits below it.
	msgsIdx := strings.Index(html, `id="chat-messages"`)
	thinkingIdx := strings.Index(html, `id="chat-thinking"`)
	if msgsIdx < 0 || thinkingIdx < 0 || msgsIdx > thinkingIdx {
		t.Errorf("thinking bubble must render after #chat-messages (msgs=%d thinking=%d)", msgsIdx, thinkingIdx)
	}
}

// TestChatListStates pins the column states: empty guidance (never a
// blank column) and the error bubble, plus the signal that hides the
// guidance once a conversation starts. The thinking spinner lives outside
// the patched container (see ChatThinking), so appends into #chat-messages
// never land below it — the reported order bug.
func TestChatListStates(t *testing.T) {
	t.Parallel()
	render := func(msgs []store.MessageView, errMsg string) string {
		t.Helper()
		var buf bytes.Buffer
		if err := chatcomponents.ChatList(msgs, errMsg).Render(context.Background(), &buf); err != nil {
			t.Fatalf("ChatList: %v", err)
		}
		return buf.String()
	}
	empty := render(nil, "")
	for _, want := range []string{`id="chat-messages"`, "Ask your app anything", `data-show="$chat_empty"`} {
		if !strings.Contains(empty, want) {
			t.Errorf("empty list missing %q", want)
		}
	}
	if strings.Contains(empty, "Thinking…") {
		t.Errorf("ChatList carries the thinking bubble: appends into #chat-messages would land below it")
	}
	var thinking bytes.Buffer
	if err := chatcomponents.ChatThinking().Render(context.Background(), &thinking); err != nil {
		t.Fatalf("ChatThinking: %v", err)
	}
	for _, want := range []string{`id="chat-thinking"`, `data-show="$chat_thinking"`, "Thinking…"} {
		if !strings.Contains(thinking.String(), want) {
			t.Errorf("thinking bubble missing %q", want)
		}
	}
	failed := render(nil, "model unavailable")
	for _, want := range []string{"model unavailable", `role="alert"`, "chat-bubble-error"} {
		if !strings.Contains(failed, want) {
			t.Errorf("error state missing %q", want)
		}
	}
}

// TestChatThreadFragmentCarriesNoContainerID is the red-proof for the
// duplicated-answer report: the fragment the SSE hub APPENDS into
// #chat-messages must not carry that id itself. Datastar patches every
// match of a server-side selector, so a nested #chat-messages made send
// #2 append into two containers and render the answer twice.
func TestChatThreadFragmentCarriesNoContainerID(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	msgs := []store.MessageView{{Role: "user", Text: "plan my day"}, {Role: "assistant", Text: "sure"}}
	if err := chatcomponents.ChatThread(msgs, "").Render(context.Background(), &buf); err != nil {
		t.Fatalf("ChatThread: %v", err)
	}
	frag := buf.String()
	if strings.Contains(frag, `id="chat-messages"`) {
		t.Errorf("appended fragment carries the container id (nests a second #chat-messages): %q", frag)
	}
	if strings.Contains(frag, `id="chat-empty"`) {
		t.Errorf("appended fragment carries the empty state: %q", frag)
	}
	for _, want := range []string{"plan my day", "sure"} {
		if !strings.Contains(frag, want) {
			t.Errorf("fragment missing bubble %q", want)
		}
	}
	// The scroll anchor travels INSIDE the fragment, last: the node the
	// patch inserts is the node that scrolls the newest answer into view,
	// so no listener and no vanilla JS is involved (see components.AutoScroll).
	if !strings.Contains(frag, "scrollIntoView") {
		t.Errorf("appended fragment carries no scroll anchor — a new answer lands off-screen: %q", frag)
	}

	buf.Reset()
	if err := chatcomponents.ChatList(nil, "").Render(context.Background(), &buf); err != nil {
		t.Fatalf("ChatList: %v", err)
	}
	if got := strings.Count(buf.String(), `id="chat-messages"`); got != 1 {
		t.Errorf("ChatList holds %d #chat-messages containers, want exactly 1", got)
	}
}

// TestChatStatePanelStepsDisagree pins the reported bug: the middle step
// never lit (no Done expression) and the first step claimed done on an
// empty thread (Done: !$chat_thinking is true when idle). The contract:
// empty = all pending, thinking = accepted done + working active,
// settled = all done.
func TestChatStatePanelStepsDisagree(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := chatcomponents.ChatStatePanel(3).Render(context.Background(), &buf); err != nil {
		t.Fatalf("ChatStatePanel: %v", err)
	}
	html := html2.UnescapeString(buf.String())
	for _, want := range []string{
		`'is-done': (!$chat_empty)`,
		`'is-done': (!$chat_thinking && !$chat_empty)`,
		`'is-active': ($chat_thinking)`,
		`Answer pipeline`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("state panel missing %q", want)
		}
	}
	if strings.Contains(html, `'is-done': (!$chat_thinking),`) {
		t.Errorf("state panel still marks Question accepted done while idle-empty")
	}
}

// TestChatNewChatAffordance pins the 2026 pattern: a New chat button (not
// "clear") with a confirmation modal before anything is wiped. The modal
// posts to /api/chat/clear and is inert while thinking.
func TestChatNewChatAffordance(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	msgs := []store.MessageView{{Role: "user", Text: "hi"}}
	if err := chatcomponents.ChatIndex(
		"a@b.c", "user", "dev", "", msgs, false, true, true, true,
	).Render(context.Background(), &buf); err != nil {
		t.Fatalf("ChatIndex: %v", err)
	}
	html := html2.UnescapeString(buf.String())
	for _, want := range []string{
		`New chat`,
		`id="confirm-new-chat"`,
		`Start a new chat?`,
		`/api/chat/clear`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("chat page missing new-chat affordance %q", want)
		}
	}
}

// TestChatListRendersBubbles proves user/assistant messages render with
// distinct alignment and that component directives render inside the
// assistant bubble (generative UI inside chat, not beside it).
func TestChatListRendersBubbles(t *testing.T) {
	t.Parallel()
	comp, err := comp.Render(comp.Directive{
		Component: "text_note",
		Props:     map[string]any{"text": "nested hi"},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	msgs := []store.MessageView{
		{Role: "user", Text: "plan my day"},
		{Role: "assistant", Text: "", Components: []templ.Component{comp}},
	}
	var buf bytes.Buffer
	if err := chatcomponents.ChatList(msgs, "").Render(context.Background(), &buf); err != nil {
		t.Fatalf("ChatList(bubbles): %v", err)
	}
	for _, want := range []string{"plan my day", "nested hi", "chat-end", "chat-start"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("bubbles missing %q", want)
		}
	}
}
