// SCOPE:plugin — keyless demo answers (Ask vs Chat) and responder selection.
package components

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/llm"
)

// TestDemoAnswersRender pins both keyless showcases: they are the only model
// output a user without a key ever sees, and nothing else guards their JSON —
// a typo would ship as an error toast in the demo. Ask's answer stays
// catalog-only; chat's carries the invented visual, which is the difference
// the two features exist to show.
func TestDemoAnswersRender(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		answer    string
		wantHTML  bool
		wantTypes []string
	}{
		"ask": {
			answer:    demoAnswer,
			wantHTML:  false,
			wantTypes: []string{"text_note", "bar_chart", "plan_cards", "action_buttons", "data_table"},
		},
		"chat": {
			answer:    demoChatAnswer,
			wantHTML:  true,
			wantTypes: []string{"html_block", "text_note"},
		},
	}
	for name, tc := range cases {
		dirs, err := ParseDirectives(tc.answer)
		if err != nil {
			t.Fatalf("%s demo answer does not parse: %v", name, err)
		}
		got := make([]string, 0, len(dirs))
		for _, d := range dirs {
			got = append(got, d.Component)
		}
		for _, want := range tc.wantTypes {
			if !slices.Contains(got, want) {
				t.Errorf("%s demo answer = %v, want %q", name, got, want)
			}
		}
		if hasHTML := slices.Contains(got, "html_block"); hasHTML != tc.wantHTML {
			t.Errorf("%s demo: html_block present = %v, want %v", name, hasHTML, tc.wantHTML)
		}
		comps, err := RenderAll(dirs)
		if err != nil {
			t.Fatalf("RenderAll(%s demo): %v", name, err)
		}
		var buf bytes.Buffer
		for _, c := range comps {
			if err := c.Render(context.Background(), &buf); err != nil {
				t.Fatalf("render %s demo: %v", name, err)
			}
		}
		rendered := buf.String()
		if !tc.wantHTML {
			continue
		}
		// The invented markup renders as HTML, not as escaped text, and it
		// renders with the sanitizer's own output: classes survive, nothing
		// is double-escaped on the way to the bubble.
		if !strings.Contains(rendered, `<div class="grid`) {
			t.Errorf("chat demo markup did not render as elements:\n%s", rendered)
		}
		if strings.Contains(rendered, "&lt;div") {
			t.Errorf("chat demo markup rendered escaped:\n%s", rendered)
		}
	}
}

// TestResponderSelection pins the keyless wiring without ever calling a
// model: a real key wins, the simulated client gets the DEMO, and neither
// configured is a fail-closed error naming the knob. The chat leg must pick
// the chat demo — collapsing both legs onto the catalog answer is exactly
// what made the two features indistinguishable.
func TestResponderSelection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	simulated := llm.New("simulated-key")
	for name, tc := range map[string]struct {
		responder Responder
		marker    string
	}{
		"ask":  {responder: NewResponder(nil, simulated, nil), marker: "connect GOAI_API_KEY for live model output"},
		"chat": {responder: NewChatResponder(nil, simulated), marker: "the model composes this markup itself"},
	} {
		got, err := tc.responder.Respond(ctx, "plan my day")
		if err != nil {
			t.Fatalf("%s demo answered with an error: %v", name, err)
		}
		if !strings.Contains(got, tc.marker) {
			t.Errorf("%s demo answer = %q, want it to contain %q", name, got, tc.marker)
		}
	}
	for name, responder := range map[string]Responder{
		"ask":  NewResponder(nil, nil, nil),
		"chat": NewChatResponder(nil, nil),
	} {
		_, err := responder.Respond(ctx, "plan my day")
		if err == nil || !strings.Contains(err.Error(), "GOAI_API_KEY") {
			t.Errorf("%s without a key = %v, want the setup error", name, err)
		}
	}
}
