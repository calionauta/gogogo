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
