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
	if err := authviews.LoginPage("/", "").Render(context.Background(), &buf); err != nil {
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
