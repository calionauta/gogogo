// The nav slot's conversion: what a recipe declares is what the rail renders.
package server

import (
	"testing"

	"github.com/calionauta/gogogo/internal/recipe"
)

// TestShellNavKeepsTheRecipeIcon pins the one field a conversion can silently
// drop: an icon is part of the destination a recipe declares, and losing it
// here is invisible to the recipe's own tests — it just renders the rail's
// generic glyph, which looks like "the icon was never supported".
func TestShellNavKeepsTheRecipeIcon(t *testing.T) {
	slots := recipe.NewSlotRegistry()
	if err := slots.Add(recipe.Contribution{
		Entries: map[recipe.SlotID][]recipe.Entry{
			recipe.SlotNavSidebar: {{
				Label: "Kiosk", Href: "/kiosk", Icon: "material-symbols:point-of-sale",
			}},
		},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	entries := shellNav(slots)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Icon != "material-symbols:point-of-sale" {
		t.Fatalf("icon = %q, want the recipe's own glyph (the rail would show the generic stack)",
			entries[0].Icon)
	}
	if entries[0].Href != "/kiosk" || entries[0].Label != "Kiosk" {
		t.Fatalf("entry = %+v, want the declared label and href", entries[0])
	}
}
