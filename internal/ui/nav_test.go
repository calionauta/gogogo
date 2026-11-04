// SCOPE:core — the rail's extension group: recipes navigate, the shell renders.
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
	var buf bytes.Buffer
	d := Data{Title: "Test", Module: "todo", Role: "user"}
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
