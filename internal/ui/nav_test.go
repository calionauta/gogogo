// The rail's extension group: recipes navigate, the shell renders.
package ui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func renderShell(ctx context.Context, t *testing.T) string {
	t.Helper()
	return renderShellRole(ctx, t, "user")
}

func renderShellRole(ctx context.Context, t *testing.T, role string) string {
	t.Helper()
	var buf bytes.Buffer
	d := Data{Title: "Test", Module: "todo", Role: role}
	//nolint:contextcheck // templ-generated Render threads the context it is given
	if err := Shell(d, templ.NopComponent, templ.NopComponent, templ.NopComponent, templ.NopComponent).
		Render(ctx, &buf); err != nil {
		t.Fatalf("render shell: %v", err)
	}
	return buf.String()
}

// TestRailRendersExtensionEntries pins the nav.sidebar slot: a contributed
// destination reaches the rail with its own href, under its own heading, and
// nothing in the markup names the recipe that contributed it.
func TestRailRendersExtensionEntries(t *testing.T) {
	ctx := WithNav(context.Background(), []NavEntry{{Label: "Pomodoro", Href: "/pomodoro"}})
	html := renderShell(ctx, t)

	for _, want := range []string{"Extensions", "Pomodoro", `href="/pomodoro"`} {
		if !strings.Contains(html, want) {
			t.Errorf("rail missing %q", want)
		}
	}
	if strings.Contains(html, "data-recipe") {
		t.Error("the rail must not carry a recipe id: attribution belongs in the receipt")
	}
}

// TestRailWithoutExtensionsRendersNoGroup keeps the empty case honest: a build
// with no contribution must not leave a stray heading behind.
func TestRailWithoutExtensionsRendersNoGroup(t *testing.T) {
	if html := renderShell(context.Background(), t); strings.Contains(html, "Extensions") {
		t.Error("a shell with no extension entry must not render the Extensions group")
	}
}

// TestRailHidesAdminOnlyExtension pins the visibility rule: an extension that
// declares itself admin-only must not render for a user or a viewer, mirroring
// how the built-in modules hide /config. Without it the rail would offer a
// destination that answers 403 — the class visibility_policy_test.go gates for
// the built-in list.
func TestRailHidesAdminOnlyExtension(t *testing.T) {
	entries := []NavEntry{
		{Label: "Pomodoro", Href: "/pomodoro"},
		{Label: "Operator tool", Href: "/ops", Admin: true},
	}
	ctx := WithNav(context.Background(), entries)

	if html := renderShellRole(ctx, t, "user"); strings.Contains(html, "/ops") {
		t.Error("a user rail rendered an admin-only extension destination")
	} else if !strings.Contains(html, "/pomodoro") {
		t.Error("a user rail dropped the public extension destination")
	}

	if html := renderShellRole(ctx, t, "admin"); !strings.Contains(html, "/ops") {
		t.Error("an admin rail must render the admin-only extension destination")
	}
}
