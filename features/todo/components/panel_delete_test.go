// SCOPE:feature — the chat panel is the native todo row, not a lookalike.
//
// The panel used to carry its own row markup and its own dialog; both drifted
// from /todo (no shared badge, no author, no age, no viewer gate, and a
// one-click delete). These tests pin the REUSE: the panel renders TodoItem and
// the page's deleteConfirmModal, with only the two write expressions scoped to
// the panel instance.
package components

import (
	stdhtml "html"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/features/todo"
)

var panelPreventAttrs = regexp.MustCompile(`data-on:click__prevent="([^"]*)"`)

func panelTestRows() []todo.Todo {
	return []todo.Todo{
		{
			ID: "row-1", Title: "buy milk", IsMine: true, CanDelete: true,
			Visibility: todo.VisibilityPrivate,
			CreatedAt:  time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC),
		},
		{
			ID: "row-2", Title: "ship it", Completed: true, IsMine: true, CanDelete: true,
			Visibility: todo.VisibilityPublic,
			CreatedAt:  time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC),
		},
	}
}

// TestPanelRendersTheNativeRow pins that the panel's rows ARE TodoItem: the
// row id/data-created, the completed strikethrough, the shared badge and the
// per-row delete affordance all come from the one component the page uses.
// Reimplementing the row (removing the '@TodoItem' call) fails these.
func TestPanelRendersTheNativeRow(t *testing.T) {
	t.Parallel()
	html := renderOrFail(t, Panel("chat-abc1", "open", panelTestRows()))
	for _, want := range []string{
		`id="chat-abc1-todo-row-1"`,
		`id="chat-abc1-todo-row-2"`,
		`class="todo-item`,
		`data-created="2026-10-21"`,
		"line-through",    // TodoItem's completed state, not a panel branch
		"Shared",          // TodoItem's visibility badge
		`class="todo-age`, // TodoItem's age, not a panel copy
	} {
		if !strings.Contains(html, want) {
			t.Errorf("panel row missing TodoItem markup %q:\n%s", want, html)
		}
	}
}

// TestPanelDeleteUsesThePageDialog pins the second half of the reuse: the
// trash opens the panel's instance of the SAME deleteConfirmModal the page
// renders (one dialog per panel, driven by the same confirming signals), and
// the confirm posts the panel's `_target` URL. A per-row copy of the dialog
// (the first fix) fails the modal-id assertion.
func TestPanelDeleteUsesThePageDialog(t *testing.T) {
	t.Parallel()
	html := stdhtml.UnescapeString(renderOrFail(t, Panel("chat-abc1", "open", panelTestRows())))

	dialogID := "chat-abc1-delete"
	if !strings.Contains(html, `id="`+dialogID+`"`) {
		t.Errorf("missing the panel's deleteConfirmModal %q", dialogID)
	}
	if got := strings.Count(html, "showModal()"); got != len(panelTestRows()) {
		t.Errorf("trash buttons opening the shared dialog = %d, want %d", got, len(panelTestRows()))
	}
	// One dialog, not one per row: the modal is the page component.
	if got := strings.Count(html, "<dialog"); got != 1 {
		t.Errorf("rendered %d dialogs, want the single shared one", got)
	}
	if !strings.Contains(html, "'/api/todos/' + $confirmingDeleteId_chat_abc1 + '/delete?filter=open") {
		t.Errorf("confirm button lost the panel delete URL:\n%s", html)
	}
	if !strings.Contains(html, `data-text="$confirmingDeleteTitle_chat_abc1"`) {
		t.Errorf("dialog body lost the panel-scoped pending title:\n%s", html)
	}
	if !strings.Contains(html, "&_target=chat-abc1") {
		t.Errorf("confirm button lost the panel slot id:\n%s", html)
	}

	// No first-click delete: a __prevent handler must only open the dialog.
	matches := panelPreventAttrs.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		t.Fatal("rendered no __prevent handler at all — the scan would be vacuous")
	}
	for _, m := range matches {
		if strings.Contains(m[1], "/delete") {
			t.Errorf("a click__prevent handler still posts a delete on first click: %.160q", m[1])
		}
	}
}

// TestPanelDeleteIsScopedToItsInstance pins the isolation: two answers in one
// thread render two dialogs with different ids, and each trash opens its own.
func TestPanelDeleteIsScopedToItsInstance(t *testing.T) {
	t.Parallel()
	first := renderOrFail(t, Panel("chat-a1", "open", panelTestRows()[0:1]))
	second := renderOrFail(t, Panel("chat-b2", "open", panelTestRows()[0:1]))
	for _, c := range []struct{ id, signal, html string }{
		{"chat-a1-delete", "confirmingDeleteId_chat_a1", first},
		{"chat-b2-delete", "confirmingDeleteId_chat_b2", second},
	} {
		if !strings.Contains(c.html, `id="`+c.id+`"`) {
			t.Errorf("panel instance missing its own dialog %q", c.id)
		}
		if !strings.Contains(c.html, c.signal) {
			t.Errorf("panel instance missing its own pending signal %q", c.signal)
		}
	}
	if strings.Contains(first, "chat-b2-delete") || strings.Contains(first, "chat_b2") ||
		strings.Contains(second, "chat-a1-delete") || strings.Contains(second, "chat_a1") {
		t.Error("a panel instance leaked the other instance's dialog/signals")
	}
}

// TestPanelEmptyCopyNamesTheSlice pins the empty state: with `open` as the
// default, "No todos to show here" would tell a user with only completed rows
// that the app lost their list.
func TestPanelEmptyCopyNamesTheSlice(t *testing.T) {
	t.Parallel()
	for filter, want := range map[string]string{
		"open": "No open todos",
		"done": "No completed todos",
		"all":  "No todos to show here",
	} {
		html := renderOrFail(t, Panel("chat-empty", filter, nil))
		if !strings.Contains(html, want) {
			t.Errorf("filter=%s empty state missing %q (got %.300q…)", filter, want, html)
		}
	}
}
