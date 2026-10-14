// SCOPE:feature — Navbar logout contract tests (red-proof).
package authviews_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/authviews"
)

// TestNavbarLogoutActuallyPosts pins the logout root cause: Datastar
// preventDefaults EVERY form submit carrying data-on:submit, so the old
// plain action=/logout form with a bare data-on:submit ran its
// expression and never POSTed — Sign out silently did nothing. The form
// must therefore drive the POST itself (fetch) and navigate by hand,
// keeping method/action as the no-JS fallback. Red-proof: revert to a
// bare data-on:submit and the fetch assertion goes red; drop the manual
// navigation and the /login assertion goes red (a Datastar @post cannot
// consume the 303 usefully).
func TestNavbarLogoutActuallyPosts(t *testing.T) {
	var buf bytes.Buffer
	nb := authviews.Navbar("demo1@demo.app", "admin", "todo", "dev", "c0ffee")
	if err := nb.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render navbar: %v", err)
	}
	html := buf.String()
	_, form, found := strings.Cut(html, `action="/logout"`)
	if !found {
		t.Fatal("navbar missing the logout form")
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

// TestNavbarGatesOperatorSurfacesToAdmin pins the role gate: the Config
// tab and the Dashboard menu item render only for an admin session, and
// the dropdown names the role so a tester can tell demo1 (admin) from
// demo2 (viewer) and read the permission the page exercises. Red-proof:
// drop the role check and the non-admin case sees /config.
func TestNavbarGatesOperatorSurfacesToAdmin(t *testing.T) {
	t.Parallel()
	render := func(role string) string {
		var buf bytes.Buffer
		nb := authviews.Navbar("demo1@demo.app", role, "todo", "dev", "c0ffee")
		if err := nb.Render(context.Background(), &buf); err != nil {
			t.Fatalf("render navbar(%q): %v", role, err)
		}
		return buf.String()
	}

	admin := render("admin")
	if !strings.Contains(admin, `href="/config"`) {
		t.Error("admin navbar must expose the operator surfaces (Config tab / Dashboard)")
	}
	if !strings.Contains(admin, "Role:") {
		t.Error("navbar must name the session role in the dropdown")
	}

	viewer := render("viewer")
	if strings.Contains(viewer, `href="/config"`) {
		t.Error("non-admin navbar must NOT link the operator Config surface")
	}
	if !strings.Contains(viewer, "viewer") {
		t.Error("viewer navbar must name the role")
	}
}
