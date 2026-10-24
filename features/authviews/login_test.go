// SCOPE:feature — Login page disclosure + demo-role contract (red-proof).
package authviews_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/authviews"
)

// TestLoginPageExplainsDemoRoles pins the permissions gap the card used
// to leave open: it named the demo pair and the shared password but never
// said what each may do. The card must now name the roles (admin /
// viewer), the Casbin mechanism, and the RBAC_ENABLED / ADMIN_EMAILS /
// VIEWER_EMAILS switch — all inside the demo-accounts removal handle so a
// product trim takes it with the rest of the showcase. Red-proof: revert
// the block and this fails.
func TestLoginPageExplainsDemoRoles(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := authviews.LoginPage("/", "", "daisyui", true, "", "").Render(context.Background(), &buf); err != nil {
		t.Fatalf("LoginPage: %v", err)
	}
	html := buf.String()
	for _, want := range []string{
		`data-demo="demo-accounts"`,
		"Demo 1", "admin", "Demo 2", "viewer",
		"Casbin", "RBAC_ENABLED", "ADMIN_EMAILS", "VIEWER_EMAILS",
		"/config",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("login page missing demo-role explanation %q", want)
		}
	}
}

// TestLoginPageHidesDemoWhenDisabled pins the production default: with the
// seed off, the page must not advertise credentials that do not exist or
// prefill them into the form. Red-proof: pass true unconditionally and the
// demo handle returns.
func TestLoginPageHidesDemoWhenDisabled(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := authviews.LoginPage("/", "", "daisyui", false, "", "").Render(context.Background(), &buf); err != nil {
		t.Fatalf("LoginPage: %v", err)
	}
	html := buf.String()
	if strings.Contains(html, `data-demo="demo-accounts"`) {
		t.Fatal("disabled demo still advertises the demo accounts")
	}
	if strings.Contains(html, "demo1@demo.app") || strings.Contains(html, "demo1234456") {
		t.Fatal("disabled demo still prefills credentials")
	}
}
