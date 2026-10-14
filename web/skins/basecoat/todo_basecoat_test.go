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
	err := TodoPage("Todos", todo.Signals{}, "a@b.c", "dev", "", true, "basecoat").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("TodoPage: %v", err)
	}
	for _, want := range []string{`id="toast-container"`, `Demonstrates:`} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("basecoat page missing %q", want)
		}
	}
}

// TestTodoPageSinglePresencePill pins one live-state slot per page on
// the basecoat skin too: the header pill duplicated the Realtime card's
// count + transport with no added meaning. Exactly one of each survives.
func TestTodoPageSinglePresencePill(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	err := TodoPage("Todos", todo.Signals{}, "a@b.c", "dev", "", true, "basecoat").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("TodoPage: %v", err)
	}
	html := buf.String()
	if n := strings.Count(html, "$connectedClients + ' online'"); n != 1 {
		t.Errorf("presence count rendered %dx, want exactly 1 (Realtime card only)", n)
	}
	if n := strings.Count(html, `data-text="$realtimeKind"`); n != 1 {
		t.Errorf("transport badge rendered %dx, want exactly 1 (Realtime card only)", n)
	}
}
