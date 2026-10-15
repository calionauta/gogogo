// SCOPE:feature — Basecoat page contract test.
package basecoat

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/todo"
)

// TestTodoPageHasToastContainer pins the basecoat toast fix: toasts
// patch #toast-container, so the skin must render the target. Without
// it every toast errors server-side (PatchElementsNoTargetsFound) and
// vanishes silently — the skin looked fine because nothing asserted it.
func TestTodoPageHasToastContainer(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	page := TodoPage("Todos", todo.Signals{}, "a@b.c", "user", "dev", "", true, "basecoat")
	err := page.Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("TodoPage: %v", err)
	}
	for _, want := range []string{`id="toast-container"`, `data-demo="tech-stack"`, `What each layer does here`} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("basecoat page missing %q", want)
		}
	}
}
