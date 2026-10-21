// SCOPE:feature — rendered-HTML invariants for the Datastar signal contract.
//
// These assertions exist because the Go-side signal values were right the whole
// time and the page was still broken. Datastar reads a signal that never
// reached the store as `""`, and its `attr` plugin treats everything that is
// not `false`/`null` as "set the attribute" — `""`, a string, and undefined
// included. So the natural-looking `data-attr:disabled="$a && !$b"` evaluates
// to `""` on a fresh page (the `&&` returns its left operand) and the control is
// BORN DISABLED: "Run durable workflow" and the delete-confirm "Delete" were
// both permanently disabled on /todo in every skin. Nothing in a Go test could
// see it, because the render was correct and only the browser's evaluation was
// wrong.
//
// The invariant is deliberately syntactic and boring: every rendered
// `data-attr:disabled` value starts with `!!`. One rule, greppable, no
// exceptions to remember — a comparison or a `.trim()` call is already boolean,
// but coercing it too costs nothing and keeps the check from needing a JS
// parser.
package components

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/calionauta/gogogo/features/todo"
	"github.com/calionauta/gogogo/web/skins/basecoat"
)

var disabledAttrRE = regexp.MustCompile(`data-attr:disabled="([^"]*)"`)

func renderOrFail(t *testing.T, comp templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := comp.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

// todoSignalsForRender seeds the async demos so the panels carrying the
// disabled attrs actually render (they are gated on the feature flags).
func todoSignalsForRender() todo.Signals {
	return todo.Signals{
		Filter:         "all",
		LLMEnabled:     true,
		SimulatedLLM:   true,
		DagNatsEnabled: true,
	}
}

func TestRenderedDisabledAttrsAreBoolean(t *testing.T) {
	t.Parallel()
	signals := todoSignalsForRender()

	cases := map[string]func() string{
		"daisyui": func() string {
			return renderOrFail(t, TodoList(signals))
		},
		"basecoat": func() string {
			return renderOrFail(t, basecoat.TodoList(signals))
		},
	}

	for skin, render := range cases {
		html := render()
		matches := disabledAttrRE.FindAllStringSubmatch(html, -1)
		if len(matches) == 0 {
			t.Fatalf("%s: rendered no data-attr:disabled at all — the invariant would be vacuous", skin)
		}
		for _, m := range matches {
			if !strings.HasPrefix(m[1], "!!") {
				t.Errorf("%s: data-attr:disabled=%q must coerce to a real boolean with !!(...); "+
					"an empty-string or undefined result SETS the attribute and disables the control",
					skin, m[1])
			}
		}
	}
}

// TestRenderedWorkflowButtonStartsEnabled pins the user-visible consequence for
// the two controls that shipped broken, so the failure names the symptom and
// not only the shape.
func TestRenderedWorkflowButtonStartsEnabled(t *testing.T) {
	t.Parallel()
	signals := todoSignalsForRender()

	for skin, render := range map[string]func() string{
		"daisyui":  func() string { return renderOrFail(t, TodoList(signals)) },
		"basecoat": func() string { return renderOrFail(t, basecoat.TodoList(signals)) },
	} {
		html := render()
		if !strings.Contains(html, "Run durable workflow") {
			t.Fatalf("%s: the durable workflow trigger is not rendered", skin)
		}
		// The expression must be false on a fresh page. Evaluating it here is
		// not possible in Go, so assert the shape that makes it false: the
		// whole thing is negated by a leading `!!(` and reads from signals that
		// are unset at first paint.
		if !strings.Contains(html, `data-attr:disabled="!!($onboardingActive && !$workflowCompleted)"`) {
			t.Errorf("%s: workflow trigger lost its boolean coercion — it renders disabled at load", skin)
		}
	}
}
