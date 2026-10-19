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
	// The resync fragment must ALSO carry the active search: the endpoint
	// treats a missing q as "no search" (and stores that on the tab view),
	// so a resync without it re-renders the full list and clears the box.
	sync := bSync.String()
	syncIdx := strings.Index(sync, "fragment?filter=")
	if syncIdx < 0 {
		t.Fatal("basecoat resync fragment URL missing")
	}
	syncWindow := sync[syncIdx:]
	if !strings.Contains(syncWindow, "$searchQuery") {
		t.Error("basecoat resync fragment URL omits $searchQuery: a resync " +
			"would re-render the full list and clear the active search")
	}
}

// TestSearchModeReaderIsDefinedOnTheBasecoatPage pins a bug the browser smoke
// caught and no render test could see: the picker and every resync path call
// searchModeValue(), but this skin is a WHOLE-PAGE replacement, so it never
// mounts features/todo/components' RealtimeRecords — the only place the daisyui
// page defines that function. The call sits inside a data-on expression, which
// Datastar evaluates at click/input time, so the page loaded fine and the search
// box then threw instead of sending. A render-level assertion is the right
// guard: it is a property of the emitted markup, not of a live browser.
//
// RED: remove @searchModeScript() from TodoPage and this fails naming it.
func TestSearchModeReaderIsDefinedOnTheBasecoatPage(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	signals := todo.Signals{
		SearchEnabled: true,
		SearchMode:    "substring",
		SearchModes:   []string{"substring", "fuzzy"},
	}
	if err := TodoPage("Todos", signals, "a@b.c", "user", "dev", "", true, "basecoat").Render(
		context.Background(), &buf); err != nil {
		t.Fatalf("TodoPage: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "window.searchModeValue") {
		t.Fatal("basecoat page calls searchModeValue() but never defines it: " +
			"every search keystroke and every resync would throw")
	}
	if n := strings.Count(html, "window.searchModeValue ="); n != 1 {
		t.Errorf("searchModeValue must be defined exactly once on the page, got %d", n)
	}
}
