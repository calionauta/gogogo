// SCOPE:feature — rendered-HTML invariants for the Datastar signal contract.
//
// These assertions exist because the Go-side signal values were right the whole
// time and the page was still broken. Datastar reads a signal that never
// reached the store as `""`, and its `attr` plugin treats everything that is
// not `false`/`null` as "set the attribute" — `""`, a string, and undefined
// included. So the natural-looking `data-attr:disabled="$a && !$b"` evaluates
// to `""` on a fresh page (the `&&` returns its left operand) and the control is
// BORN DISABLED: "Run durable workflow" and the delete-confirm "Delete" were
// both permanently disabled on /todo. Nothing in a Go test could
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

	// Only trees THIS package owns: importing another package's tree would
	// make this package's tests unbuildable (lived: TestTrimmedTreeStillBuilds
	// failed for exactly that shape).
	html := renderOrFail(t, TodoStatePanel(signals))
	matches := disabledAttrRE.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		t.Fatal("rendered no data-attr:disabled at all — the invariant would be vacuous")
	}
	for _, m := range matches {
		if !strings.HasPrefix(m[1], "!!") {
			t.Errorf("data-attr:disabled=%q must coerce to a real boolean with !!(...); "+
				"an empty-string or undefined result SETS the attribute and disables the control",
				m[1])
		}
	}
}

// TestRenderedWorkflowButtonStartsEnabled pins the user-visible consequence for
// the two controls that shipped broken, so the failure names the symptom and
// not only the shape.
func TestRenderedWorkflowButtonStartsEnabled(t *testing.T) {
	t.Parallel()
	signals := todoSignalsForRender()

	html := renderOrFail(t, TodoStatePanel(signals))
	if !strings.Contains(html, "Run durable workflow") {
		t.Fatal("the durable workflow trigger is not rendered")
	}
	// The expression must be false on a fresh page. Evaluating it here is
	// not possible in Go, so assert the shape that makes it false: the
	// whole thing is negated by a leading `!!(` and reads from signals that
	// are unset at first paint.
	if !strings.Contains(html, `data-attr:disabled="!!($onboardingActive && !$workflowCompleted)"`) {
		t.Error("workflow trigger lost its boolean coercion — it renders disabled at load")
	}
}
