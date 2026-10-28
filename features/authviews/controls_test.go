// SCOPE:core — Shell control contracts (red-proof).
//
// These used to live on Navbar, which is gone: the shell renders the account
// menu on every page, landing included, so the contract is asserted on the
// component the shell composes.
package authviews_test

import (
	"bytes"
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/authviews"
	"github.com/calionauta/gogogo/web/resources"
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
	if !strings.Contains(form, "__gogogoLogout") {
		t.Error("logout form must delegate to the transport helper (queue-aware sign-out)")
	}
	// The helper owns the POST/navigation so it can ask about queued changes
	// first. Pinning it here keeps the delegation from becoming a dead form:
	// a native submit would land on /login, but a queued mutation would then
	// replay under the next account on a shared device.
	script, err := fs.ReadFile(resources.StaticFS(), "offline-banner.js")
	if err != nil {
		t.Fatalf("read offline-banner.js: %v", err)
	}
	for _, want := range []string{
		`fetch("/logout"`, `method: "POST"`, `window.location.href = "/login"`, `"clear-user-data"`,
	} {
		if !strings.Contains(string(script), want) {
			t.Errorf("offline-banner.js missing the sign-out contract %q", want)
		}
	}
}

// The admin gate that used to live here (Config tab visible only for an admin)
// moved with the navigation: the sidebar renders []Module, and
// internal/ui/modules_test.go pins both directions (the flag and the policy's
// admin-only route set must agree).

// TestUserMenuCarriesConfigOnlyForAdmins pins the account menu's operator
// entry. Config is MenuOnly in the module registry (it has no rail item), so
// this menu is its one entry point — and a plain user must not be offered a
// route whose policy answers 403.
func TestUserMenuCarriesConfigOnlyForAdmins(t *testing.T) {
	render := func(role string) string {
		t.Helper()
		var buf bytes.Buffer
		if err := authviews.UserMenu("demo1@demo.app", role, nil).
			Render(context.Background(), &buf); err != nil {
			t.Fatalf("render user menu (%s): %v", role, err)
		}
		return buf.String()
	}
	for _, role := range []string{"admin"} {
		if !strings.Contains(render(role), `href="/config"`) {
			t.Errorf("%s menu missing the Config entry", role)
		}
	}
	for _, role := range []string{"user", "viewer", ""} {
		if strings.Contains(render(role), `href="/config"`) {
			t.Errorf("role %q is offered /config (the policy denies it: a click 403s)", role)
		}
	}
}
