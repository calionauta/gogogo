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
	render := func(role string) string {
		t.Helper()
		var buf bytes.Buffer
		if err := authviews.CommandPalette(role).Render(context.Background(), &buf); err != nil {
			t.Fatalf("render palette: %v", err)
		}
		return buf.String()
	}

	admin := render("admin")
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

	if strings.Contains(render("viewer"), `href="/config"`) {
		t.Error("non-admin palette must not link the operator Config surface")
	}
}
