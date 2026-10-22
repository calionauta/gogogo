// SCOPE:feature — the same rendered-HTML boolean invariant as the DaisyUI tree,
// asserted from inside the skin so a trimmed build (skins-extra deletes this
// whole package) takes the test with it. An import of this package from a
// shared test package is what broke TestTrimmedTreeStillBuilds/skins-extra.
//
// Why it matters here: Datastar reads a signal that never reached the store as
// `""`, and its `attr` plugin SETS the attribute for anything that is not
// `false`/`null`. So `data-attr:disabled="$a && !$b"` evaluates to `""` on a
// fresh page and the control is born dead — "Run durable workflow" and the
// delete-confirm "Delete" both shipped that way in this skin.
package basecoat

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/todo"
)

var disabledAttrRE = regexp.MustCompile(`data-attr:disabled="([^"]*)"`)

func renderTodoList(t *testing.T) string {
	t.Helper()
	signals := todo.Signals{
		Filter:         "all",
		LLMEnabled:     true,
		SimulatedLLM:   true,
		DagNatsEnabled: true,
	}
	var buf bytes.Buffer
	if err := TodoList(signals).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render TodoList: %v", err)
	}
	return buf.String()
}

func TestRenderedDisabledAttrsAreBoolean(t *testing.T) {
	t.Parallel()
	html := renderTodoList(t)

	matches := disabledAttrRE.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		t.Fatalf("rendered no data-attr:disabled at all — the invariant would be vacuous")
	}
	for _, m := range matches {
		if !strings.HasPrefix(m[1], "!!") {
			t.Errorf("data-attr:disabled=%q must coerce to a real boolean with !!(...); "+
				"an empty-string or undefined result SETS the attribute and disables the control", m[1])
		}
	}
}

// TestRenderedWorkflowButtonStartsEnabled names the symptom, not only the shape.
func TestRenderedWorkflowButtonStartsEnabled(t *testing.T) {
	t.Parallel()
	html := renderTodoList(t)

	if !strings.Contains(html, "Run durable workflow") {
		t.Fatalf("the durable workflow trigger is not rendered")
	}
	if !strings.Contains(html, `data-attr:disabled="!!($onboardingActive && !$workflowCompleted)"`) {
		t.Errorf("workflow trigger lost its boolean coercion — it renders disabled at load")
	}
}
