// Request shape for the todo list's background GETs.
package components

import (
	"strings"
	"testing"
)

// TestListRequestsDoNotShipTheSignalBlob pins the two properties every
// background GET needs, in the page that has the biggest signal set:
//
//   - no `datastar=` param. Datastar appends the WHOLE signal state to every
//     action by default; on /todo that included the todos array, producing a
//     multi-KB URL on each tab click and putting user data in proxy logs.
//
// Retries stay at the default here: a list GET is idempotent, viewers have read
// access by policy, and a transient 5xx is what the retry is for. The badge is
// the one action that must not retry (see TestViewerGetsNoInboxPoll).
func TestListRequestsDoNotShipTheSignalBlob(t *testing.T) {
	t.Parallel()
	for name, expr := range map[string]string{
		"filter":       TodoFilterOn("active"),
		"scope":        TodoScopeOn("mine"),
		"scope select": TodoScopeSelectOn(),
		"search":       TodoSearchOn(),
		"search clear": TodoSearchClearOn(),
	} {
		if !strings.Contains(expr, "filterSignals: {exclude: /.*/}") {
			t.Errorf("%s request does not exclude the signal blob: %s", name, expr)
		}
	}
}
