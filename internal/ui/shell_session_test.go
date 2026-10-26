// SCOPE:core — shell session-recovery contract.
package ui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

// TestShellRecoversAnExpiredSession pins the client half of the 401 story:
// an action whose session expired between page loads must land on the login
// page (returning here), not die silently. The server bounces page loads with
// a redirect; only the scripted actions need this handler, and nothing else
// asserts it exists.
func TestShellRecoversAnExpiredSession(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	d := Data{Title: "Todo", Module: "todo", Email: "user@example.com", Role: "user"}
	err := Shell(d,
		templ.NopComponent, templ.NopComponent, templ.NopComponent, templ.NopComponent,
	).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render shell: %v", err)
	}
	html := buf.String()
	for _, want := range []string{
		`data-on:datastar-fetch__document`,
		`argsRaw.status === '401'`,
		`'/login?next=' + encodeURIComponent`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("shell missing the expired-session recovery %q", want)
		}
	}
	// The banner hashes this into the queue owner, so it must reach the page.
	if !strings.Contains(html, `data-user-email="user@example.com"`) {
		t.Error("shell did not pass the session email to the offline banner")
	}
}
