// SCOPE:feature — Email-link landing/form contracts.
package authviews_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/authviews"
)

func TestMagicConfirmPageRendersAccountAndForm(t *testing.T) {
	var buf bytes.Buffer
	if err := authviews.MagicConfirmPage("user@example.com", "tok-1", "bind-1", "").
		Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()
	for _, want := range []string{
		"user@example.com", `action="/api/auth/magic"`, `name="token" value="tok-1"`,
		`name="bind" value="bind-1"`, "If you didn't request this",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("magic confirm missing %q", want)
		}
	}
}

func TestPasswordResetPageRendersForm(t *testing.T) {
	var buf bytes.Buffer
	if err := authviews.PasswordResetPage("user@example.com", "tok-2", "Too short").
		Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()
	for _, want := range []string{
		"user@example.com", `action="/api/auth/password/reset"`, `name="token" value="tok-2"`,
		"Too short", `name="confirm"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("reset page missing %q", want)
		}
	}
}
