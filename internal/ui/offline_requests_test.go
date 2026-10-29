// SCOPE:core — request shape for background GETs (signals in the URL, retries).
package ui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

// TestListRequestsDoNotShipTheSignalBlob pins the two properties every
// background GET needs, in the page that has the biggest signal set:
//
//   - no `datastar=` param. Datastar appends the WHOLE signal state to every
//     action by default; on /todo that included the todos array, producing a
//     multi-KB URL on each tab click and putting user data in proxy logs.
//   - no automatic retries. A refused or failed list is re-requested by the next
//     user action; the default ten retries turned one 403 into a console storm.
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
		if !strings.Contains(expr, "retryMaxCount: 0") {
			t.Errorf("%s request retries automatically: %s", name, expr)
		}
	}
}

// TestViewerGetsNoInboxPoll pins the role fence on the badge refresh: the badge
// route is `user`+ in policy.csv, so a viewer's poll is a guaranteed 403 on
// every focus/visibility/online event. The control must not render at all.
func TestViewerGetsNoInboxPoll(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		role string
		want bool
	}{
		{"viewer", false},
		{"user", true},
		{"admin", true},
	} {
		var buf bytes.Buffer
		d := Data{Title: "Todo", Module: "todo", Email: "who@example.com", Role: tc.role}
		if err := Shell(d, templ.NopComponent, templ.NopComponent, templ.NopComponent, templ.NopComponent).
			Render(context.Background(), &buf); err != nil {
			t.Fatalf("%s: render shell: %v", tc.role, err)
		}
		html := buf.String()
		has := strings.Contains(html, `id="rail-inbox-refresh"`)
		if has != tc.want {
			t.Errorf("%s: inbox refresh rendered = %v, want %v", tc.role, has, tc.want)
		}
		if !has {
			continue
		}
		if !strings.Contains(html, "filterSignals: {exclude: /.*/}") {
			t.Errorf("%s: badge poll ships the signal blob", tc.role)
		}
		if !strings.Contains(html, "retryMaxCount: 0") {
			t.Errorf("%s: badge poll retries a refusal", tc.role)
		}
	}
}
