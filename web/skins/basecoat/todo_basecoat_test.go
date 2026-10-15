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

// TestBasecoatMutationURLsPreserveSlice mirrors the daisyui slice
// contract: toggle/clear/delete/resync endpoints carry filter + scope
// (+ clientID), so acting from Mine/Shared never yanks the tab back.
// Red-proof: drop &scope= from any URL and this fails naming it.
func TestBasecoatMutationURLsPreserveSlice(t *testing.T) {
	t.Parallel()
	signals := todo.Signals{
		Filter: "all", Scope: "mine", ItemCount: 1,
		Todos: []todo.Todo{{
			ID: "x1", Title: "probe", Owner: "me",
			Visibility: todo.VisibilityPrivate, IsMine: true, CanDelete: true,
		}},
	}
	var bList bytes.Buffer
	if err := TodoList(signals).Render(context.Background(), &bList); err != nil {
		t.Fatalf("render basecoat TodoList: %v", err)
	}
	var bItem bytes.Buffer
	if err := todoItem(signals.Todos[0]).Render(context.Background(), &bItem); err != nil {
		t.Fatalf("render basecoat todoItem: %v", err)
	}
	// The realtime resync path renders separately (TodoPage mounts it
	// outside the list), so it gets its own render surface.
	var bSync bytes.Buffer
	if err := realtimeResync().Render(context.Background(), &bSync); err != nil {
		t.Fatalf("render basecoat realtimeResync: %v", err)
	}
	page := bList.String() + bItem.String() + bSync.String()
	for _, endpoint := range []string{"toggle?", "completed/delete?", "/delete?clientID=", "fragment?filter="} {
		idx := strings.Index(page, endpoint)
		if idx < 0 {
			t.Errorf("basecoat markup missing endpoint %q", endpoint)
			continue
		}
		window := page[idx : idx+320]
		if !strings.Contains(window, "scope=") {
			t.Errorf("basecoat endpoint %q must carry &scope= (got %.120q…)", endpoint, window)
		}
	}
}
