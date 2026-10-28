// SCOPE:feature — the chat panel's row actions follow the same contract as a
// row on /todo: toggle in place, and ASK before a permanent delete.
package components

import (
	"regexp"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/todo"
)

var panelPreventAttrs = regexp.MustCompile(`data-on:click__prevent="([^"]*)"`)

// TestPanelDeleteAsksBeforePosting is the red-proof for the reported bug: a
// chat answer's trash deleted on the first click while the same row on /todo
// opened the confirmation dialog. The panel renders wherever a chat answer
// does, so it carries its own dialog (the page's #confirm-delete-modal is not
// on that page).
//
// Red-proof: put the @post back on the trash's data-on:click__prevent and both
// the dialog-id assertion and the "no delete in a __prevent handler" scan fail.
func TestPanelDeleteAsksBeforePosting(t *testing.T) {
	t.Parallel()
	rows := []todo.PanelRows{
		{ID: "row-1", Title: "buy milk"},
		{ID: "row-2", Title: "ship it", Done: true},
	}
	html := renderOrFail(t, Panel("chat-abc1", "open", rows))

	if got := strings.Count(html, "showModal()"); got != len(rows) {
		t.Errorf("trash buttons opening a dialog = %d, want %d", got, len(rows))
	}
	for _, row := range rows {
		dialogID := panelDeleteDialogID("chat-abc1", row.ID)
		if !strings.Contains(html, `id="`+dialogID+`"`) {
			t.Errorf("missing confirmation dialog %q for %q", dialogID, row.Title)
		}
		if !strings.Contains(html, "/api/todos/"+row.ID+"/delete?filter=open") {
			t.Errorf("confirm button for %q lost the delete endpoint", row.Title)
		}
	}
	// The trash is the only affordance using click__prevent for a delete; the
	// confirm button uses plain click. A delete in a __prevent handler is the
	// first-click delete this test exists to forbid.
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
