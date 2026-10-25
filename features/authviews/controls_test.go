// SCOPE:core — Shell control contracts (red-proof).
//
// These used to live on Navbar, which is gone: the shell renders the account
// menu on every module page and the landing renders it on the public header, so
// the contract is asserted on the component both use.
package authviews_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/authviews"
)

func TestUserMenuLogoutActuallyPosts(t *testing.T) {
	var buf bytes.Buffer
	if err := authviews.UserMenu("demo1@demo.app", "admin", nil).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render user menu: %v", err)
	}
	html := buf.String()
	_, form, found := strings.Cut(html, `action="/logout"`)
	if !found {
		t.Fatal("user menu missing the logout form")
	}
	formStart := strings.LastIndex(html[:len(html)-len(form)], "<form")
	if formStart < 0 {
		t.Fatal("logout action has no form element")
	}
	form, _, found = strings.Cut(html[formStart:], "</form>")
	if !found {
		t.Fatal("logout form never closes")
	}
	if !strings.Contains(form, `method="POST"`) {
		t.Error("logout form must keep method=POST as the no-JS fallback")
	}
	if !strings.Contains(form, "data-on:submit__prevent") {
		t.Error("logout form must prevent the dead native submit explicitly")
	}
	if !strings.Contains(form, "fetch('/logout', {method: 'POST'") {
		t.Error("logout form must POST via fetch (native submit is swallowed by Datastar)")
	}
	if !strings.Contains(form, "window.location.href = '/login'") {
		t.Error("logout form must navigate to /login by hand after the POST")
	}
	if !strings.Contains(form, "clear-pages") {
		t.Error("logout form must still tell the service worker to drop cached pages")
	}
}

// The admin gate that used to live here (Config tab visible only for an admin)
// moved with the navigation: the sidebar renders []Module, and
// internal/ui/modules_test.go pins both directions (the flag and the policy's
// admin-only route set must agree).
