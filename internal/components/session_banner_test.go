// SCOPE:core — SessionBanner render test.
package components_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/components"
)

// TestSessionBannerCarriesAuthedAndScript pins the shared session banner's
// wiring: the element reports the render-time auth state (so a public page
// never warns about a session it never had) and loads the shared controller
// exactly once. Every authenticated page renders this same component.
func TestSessionBannerCarriesAuthedAndScript(t *testing.T) {
	render := func(authed bool) string {
		t.Helper()
		var buf bytes.Buffer
		if err := components.SessionBanner(authed).Render(context.Background(), &buf); err != nil {
			t.Fatalf("render(authed=%v): %v", authed, err)
		}
		return buf.String()
	}

	authedHTML := render(true)
	for _, want := range []string{
		`id="session-banner"`,
		`data-authed="true"`,
		"/static/session.js",
		`class="hidden`,
		"sign in again",
	} {
		if !strings.Contains(authedHTML, want) {
			t.Errorf("SessionBanner(true) missing %q", want)
		}
	}

	if !strings.Contains(render(false), `data-authed="false"`) {
		t.Errorf("SessionBanner(false) missing data-authed=false")
	}
}
