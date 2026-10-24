// Command palette render contract: the dialog, the static commands, and the
// admin gate. Behaviour (Ctrl/Cmd+K, arrow keys, filtering) is browser-level
// and covered by the UI review, not by this render test.
package authviews_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/authviews"
)

func TestCommandPaletteRendersCommandsAndAdminGate(t *testing.T) {
	render := func(role string, guest bool) string {
		t.Helper()
		var buf bytes.Buffer
		if err := authviews.CommandPalette(role, guest).Render(context.Background(), &buf); err != nil {
			t.Fatalf("render palette: %v", err)
		}
		return buf.String()
	}

	admin := render("admin", false)
	for _, want := range []string{
		`id="command-palette"`,
		`data-on:keydown__window`,
		`id="command-palette-list"`,
		`role="combobox"`,
		`id="command-palette-input"`,
		`<label class="input`,
		`>esc</kbd>`,
		`⌘</kbd>`,
		`>K</kbd>`,
		`data-command`,
		`href="/todo"`,
		`href="/config"`,
	} {
		if !strings.Contains(admin, want) {
			t.Errorf("admin palette missing %q", want)
		}
	}

	if strings.Contains(render("viewer", false), `href="/config"`) {
		t.Error("non-admin palette must not link the operator Config surface")
	}

	// Guests never reach app paths directly (RBAC denies anonymous with 403),
	// so every guest link must route through login with next preserved.
	guest := render("", true)
	for _, want := range []string{
		`href="/login?next=/todo"`,
		`href="/login?next=/whiteboard"`,
		`href="/login?next=/notes"`,
		`href="/login?next=/chat"`,
		`href="/login?next=/review"`,
		`href="/login?next=/room"`,
	} {
		if !strings.Contains(guest, want) {
			t.Errorf("guest palette missing %q (would 403 on the app path)", want)
		}
	}
	for _, banned := range []string{
		`href="/todo"`,
		`href="/whiteboard"`,
		`href="/notes"`,
		`href="/chat"`,
		`href="/review"`,
		`href="/room"`,
	} {
		if strings.Contains(guest, banned) {
			t.Errorf("guest palette leaks direct app link %q (anonymous 403s)", banned)
		}
	}
}
