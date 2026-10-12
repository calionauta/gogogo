// SCOPE:feature — Theme toggle contract (ported from features/auth/auth_test.go)
//
// The PB-era suite rendered the navbar from features/authviews and pinned
// the single-toggle path (signal flip, icon sync, persistence order) plus
// the shared storage key across ThemeHead, the button expression, and the
// theme.js module — with one owner, one button, one storage key. The test
// lived in features/auth and referenced its sibling theme_head.templ,
// which moved to features/authviews in the flat port; the file died
// referencing a path that no longer existed (red on bb/chat-demo). This
// is the port, rooted where the templates actually live.
package authviews_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/authviews"
)

// TestThemeToggleSingleOwner pins the shared theme-toggle contract.
func TestThemeToggleSingleOwner(t *testing.T) {
	var buf bytes.Buffer
	if err := authviews.Navbar("demo1@demo.app", "todo", "dev", "c0ffee").Render(context.Background(), &buf); err != nil {
		t.Fatalf("render navbar: %v", err)
	}
	html := buf.String()
	start := strings.Index(html, "app-theme-toggle")
	if start < 0 {
		t.Fatal("navbar missing the theme toggle button")
	}
	end := strings.Index(html[start:], "</button>")
	if end < 0 {
		t.Fatal("navbar theme button never closes")
	}
	button := html[start : start+end]
	if !strings.Contains(button, "theme-toggle") {
		t.Error("theme button missing the .theme-toggle class")
	}
	// The single toggle path: signal flip (Datastar owns data-theme),
	// icon sync, persistence — in that order so a storage throw in
	// private mode cannot skip the visible change.
	for _, want := range []string{
		`data-on:click="`,
		`$theme = $theme === 'dark' ? 'light' : 'dark'`,
		`Theme.syncIcons($theme)`,
		`localStorage.setItem('themeMode', $theme)`,
	} {
		if !strings.Contains(button, want) {
			t.Errorf("theme button missing %q (the single toggle path)", want)
		}
	}

	// One storage key across the pre-paint bootstrap and the button.
	head, err := os.ReadFile("theme_head.templ")
	if err != nil {
		t.Fatalf("read theme_head: %v", err)
	}
	if !strings.Contains(string(head), "themeMode") {
		t.Error("ThemeHead left the shared storage key")
	}
	if !strings.Contains(button, "themeMode") {
		t.Error("button expression left the shared storage key")
	}
	js, err := os.ReadFile("../../web/resources/static/theme.js")
	if err != nil {
		t.Fatalf("read theme.js: %v", err)
	}
	if strings.Contains(string(js), `setAttribute("data-theme"`) {
		t.Error("theme.js must not write data-theme (Datastar reverts " +
			"external writes to bound attributes; the $theme signal owns it)")
	}
}
