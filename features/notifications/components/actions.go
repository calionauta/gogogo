// SCOPE:feature — notifications page helpers.
package components

import "strconv"

// unreadLabel is the badge copy. The count is stated even at zero, so the
// badge cannot be mistaken for an error state.
func unreadLabel(unread int) string {
	if unread == 0 {
		return "none new"
	}
	if unread == 1 {
		return "1 new"
	}
	return strconv.Itoa(unread) + " new"
}

// clientIDExpr appends this tab's clientID to an action URL. The server uses it
// to skip the tab that already rendered the change, so a click does not get a
// second, identical patch from its own stream. It starts with the JS `+`
// operator and assumes the caller closed its URL string literal first.
const clientIDExpr = " + '?clientID=' + encodeURIComponent(window.__gogogoClientID || '')"

// readAction is the Datastar action that marks one row read. The quote after
// the path closes the URL literal before clientIDExpr concatenates: without it
// the whole argument parses as `'/path + '` followed by a bare `?clientID=`,
// which is the syntax error the browser reported as CSP/GenerateExpression.
func readAction(id string) string {
	return "@post('/api/notifications/" + id + "/read'" + clientIDExpr + ")"
}

// readAllAction marks every in-app row read.
func readAllAction() string {
	return "@post('/api/notifications/read-all'" + clientIDExpr + ")"
}
