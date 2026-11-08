// Notifications page helpers.
package components

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

// unreadAction is the Datastar action that marks one row unread again, by the
// same single-quoted-URL rule as readAction.
func unreadAction(id string) string {
	return "@post('/api/notifications/" + id + "/unread'" + clientIDExpr + ")"
}

// readAllAction marks every in-app row read.
func readAllAction() string {
	return "@post('/api/notifications/read-all'" + clientIDExpr + ")"
}

// rowShow is the $notificationFilter expression one row carries in data-show.
// The inbox never paginates, so the filter is client-side: read rows leave
// the DOM's visibility when the tab is "unread" and come back when it is
// "all". The expression travels with the row markup, so a stream or action
// patch that re-renders #inbox re-binds the current filter on its own — no
// server round-trip per tab click.
func rowShow(unread bool) string {
	literal := "false"
	if unread {
		literal = "true"
	}
	return "$notificationFilter !== 'unread' || " + literal
}
