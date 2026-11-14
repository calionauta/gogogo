// A recipe's row action reaches the row with its own id.
package components

import (
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/todo"
)

func renderRow(t *testing.T, item todo.Todo, extras []todo.RowAction) string {
	t.Helper()
	var b strings.Builder
	actions := TodoRowActions{IDPrefix: "todo-", Extras: extras}
	if err := TodoItem(item, actions).Render(context.Background(), &b); err != nil {
		t.Fatalf("render row: %v", err)
	}
	return b.String()
}

// TestRowRendersRecipeActionWithItsOwnID pins the whole point of the slot: the
// action the recipe declared carries THIS row's id, so the endpoint it posts to
// names the row instead of "some row".
func TestRowRendersRecipeActionWithItsOwnID(t *testing.T) {
	item := todo.Todo{ID: "01abc", Title: "Write the plan"}
	html := renderRow(t, item, []todo.RowAction{
		{Label: "Send to timer", Href: "/api/timer/from-todo/{id}", Verb: "post", Confirm: "Send this row?"},
	})

	if !strings.Contains(html, "Send to timer") {
		t.Fatalf("the action did not render:\n%s", html)
	}
	if !strings.Contains(html, "/api/timer/from-todo/01abc") {
		t.Errorf("the row id was not substituted into the action href:\n%s", html)
	}
	if strings.Contains(html, todo.RowIDPlaceholder) {
		t.Error("a literal {id} reached the page: the action would post to a placeholder")
	}
	if !strings.Contains(html, "Send this row?") {
		t.Error("the confirmation the recipe declared is not in the expression")
	}
}

// TestRowWithoutRecipeActionsIsUnchanged keeps the empty case honest: a build
// with no installed recipe renders the row it always rendered.
func TestRowWithoutRecipeActionsIsUnchanged(t *testing.T) {
	html := renderRow(t, todo.Todo{ID: "01abc", Title: "Plain"}, nil)
	if strings.Contains(html, "@post(") && strings.Contains(html, "/api/timer") {
		t.Errorf("a row with no contribution rendered a recipe action:\n%s", html)
	}
}

// TestGetActionRendersALink pins the verb split: a navigating action is an
// anchor (middle-click, open in new tab), and only a mutating one is a button.
func TestGetActionRendersALink(t *testing.T) {
	html := renderRow(t, todo.Todo{ID: "01abc", Title: "x"}, []todo.RowAction{
		{Label: "Open in timer", Href: "/timer/{id}", Verb: "get"},
	})
	if !strings.Contains(html, `href="/timer/01abc"`) {
		t.Errorf("a get action must render as an anchor with the row id:\n%s", html)
	}
	if strings.Contains(html, "@post(") {
		t.Error("a get action must not post")
	}
}
