// SCOPE:feature — Login page stack-disclosure test.
package authviews_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/authviews"
)

// TestLoginPageDisclosesAuthStack pins the login TechBadge: the page
// demonstrates owned credential auth (argon2id password hashing +
// session cookie, no provider), and that must read identically to every
// other feature page — same component, same demo-only handle.
func TestLoginPageDisclosesAuthStack(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := authviews.LoginPage("/", "").Render(context.Background(), &buf); err != nil {
		t.Fatalf("LoginPage: %v", err)
	}
	html := buf.String()
	for _, want := range []string{
		"Demonstrates:", "password auth", "argon2id hashing", "owned sessions",
		`data-demo="tech-stack"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("login page missing stack disclosure %q", want)
		}
	}
}

// TestLoginPageExplainsDemoRoles is the red-proof for the permissions
// gap: the login card named the demo accounts but never said what they
// may do — and demo1/demo2 carry no fixed role (roles come from the
// server's ADMIN_EMAILS/VIEWER_EMAILS boot lists). The card must name
// the Casbin exercise explicitly, inside the demo-accounts removal
// handle so product trims take it with the rest of the showcase.
func TestLoginPageExplainsDemoRoles(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := authviews.LoginPage("/", "").Render(context.Background(), &buf); err != nil {
		t.Fatalf("LoginPage: %v", err)
	}
	html := buf.String()
	for _, want := range []string{
		`data-demo="demo-accounts"`, "VIEWER_EMAILS", "RBAC_ENABLED",
		"no fixed role", "/config", "admin-only",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("login page missing demo-role explanation %q", want)
		}
	}
}
