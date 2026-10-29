// SCOPE:core — Shell control contracts (red-proof).
//
// These used to live on Navbar, which is gone: the shell renders the account
// menu on every page, landing included, so the contract is asserted on the
// component the shell composes.
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
	if err := authviews.UserMenu("demo1@demo.app", "admin", nil, false).Render(context.Background(), &buf); err != nil {
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
	// The submit handler owns the POST and the navigation so it can ask about
	// queued changes first: a native submit would land on /login, but a queued
	// intent would then replay under the next account on a shared device. The
	// storage purge itself is the server's Clear-Site-Data response header
	// (asserted in the handlers package), so the handler only confirms and
	// posts — no script file, no second state model.
	for _, want := range []string{`@post('/logout')`, `confirm(`, `$bannerCount`, `window.location.href = '/login'`} {
		if !strings.Contains(form, want) {
			t.Errorf("logout form missing the sign-out contract %q", want)
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
		if err := authviews.UserMenu("demo1@demo.app", role, nil, false).
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

// TestUserMenuCarriesDagNatsConsoleOnlyForOperators pins the console entry:
// the fence is admin AND an ADMIN_EMAILS match, so an admin who is not an
// operator (the published demo account) must not be offered a link that
// answers 403. The entry was dropped entirely when the fence landed; restoring
// it for everyone would ship the same dead link.
func TestUserMenuCarriesDagNatsConsoleOnlyForOperators(t *testing.T) {
	render := func(operator bool) string {
		t.Helper()
		var buf bytes.Buffer
		if err := authviews.UserMenu("demo1@demo.app", "admin", nil, operator).
			Render(context.Background(), &buf); err != nil {
			t.Fatalf("render user menu (operator=%v): %v", operator, err)
		}
		return buf.String()
	}
	if !strings.Contains(render(true), `href="/dagnats/"`) {
		t.Error("operator menu is missing the DagNats console entry")
	}
	if strings.Contains(render(false), `href="/dagnats/"`) {
		t.Error("a non-operator admin is offered /dagnats (the fence answers 403)")
	}
}
