// SCOPE:feature — a todo row renders the actions recipes contributed.
//
// The row is shared between /todo and the chat panel, so the substitution of a
// row id happens in one place: a second implementation would drift the moment
// either host changed how it names its rows.
package components

import (
	"strings"

	"github.com/calionauta/gogogo/features/todo"
)

// actionHref substitutes the row id into the recipe's declared template. The
// template was validated at registration to carry exactly one placeholder, so
// a plain replace cannot leave an ambiguous target behind.
func actionHref(a todo.RowAction, id string) string {
	return strings.ReplaceAll(a.Href, todo.RowIDPlaceholder, id)
}

// actionPost builds the Datastar expression for a mutating action.
//
// The href and the confirmation are both quoted as JSON strings, never
// concatenated raw: the path is a recipe's, and a quote in it would otherwise
// end the expression and run as code.
func actionPost(a todo.RowAction, id string) string {
	post := "@post(" + safeJSString(actionHref(a, id)) + ")"
	if a.Confirm == "" {
		return post
	}
	return "if (confirm(" + safeJSString(a.Confirm) + ")) { " + post + " }"
}
