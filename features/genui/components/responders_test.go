// SCOPE:plugin — keyless demo answer and responder selection.
package components

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/llm"
)

// TestDemoAnswerRenders pins the keyless showcase: it is the only model
// output a user without a key ever sees, and nothing else guards its JSON —
// a typo would ship as an error toast in the demo. It carries the invented
// visual.
func TestDemoAnswerRenders(t *testing.T) {
	t.Parallel()
	dirs, err := ParseDirectives(demoAnswer)
	if err != nil {
		t.Fatalf("demo answer does not parse: %v", err)
	}
	got := make([]string, 0, len(dirs))
	for _, d := range dirs {
		got = append(got, d.Component)
	}
	for _, want := range []string{"html_block", "text_note"} {
		if !slices.Contains(got, want) {
			t.Errorf("demo answer = %v, want %q", got, want)
		}
	}
	comps, err := RenderAll(dirs)
	if err != nil {
		t.Fatalf("RenderAll(demo): %v", err)
	}
	var buf bytes.Buffer
	for _, c := range comps {
		if err := c.Render(context.Background(), &buf); err != nil {
			t.Fatalf("render demo: %v", err)
		}
	}
	rendered := buf.String()
	// The invented markup renders as HTML, not as escaped text, and it
	// renders with the sanitizer's own output: classes survive, nothing
	// is double-escaped on the way to the bubble.
	if !strings.Contains(rendered, `<div class="grid`) {
		t.Errorf("demo markup did not render as elements:\n%s", rendered)
	}
	if strings.Contains(rendered, "&lt;div") {
		t.Errorf("demo markup rendered escaped:\n%s", rendered)
	}
}

// TestResponderSelection pins the keyless wiring without ever calling a
// model: a real key wins, the simulated client gets the DEMO, and neither
// configured is a fail-closed error naming the knob.
func TestResponderSelection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	simulated := llm.New("simulated-key")
	responder := NewResponder(nil, simulated, nil, nil)
	got, err := responder.Respond(ctx, "plan my day")
	if err != nil {
		t.Fatalf("demo answered with an error: %v", err)
	}
	if !strings.Contains(got, "the model composes this markup itself") {
		t.Errorf("demo answer = %q, want the invented-visual marker", got)
	}
	_, err = NewResponder(nil, nil, nil, nil).Respond(ctx, "plan my day")
	if err == nil || !strings.Contains(err.Error(), "GOAI_API_KEY") {
		t.Errorf("without a key = %v, want the setup error", err)
	}
}
