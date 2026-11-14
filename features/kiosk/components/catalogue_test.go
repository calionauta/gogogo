// The catalogue: every sellable option is visible and selectable, and a tap
// produces the same order a spoken phrase would.
package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestCatalogueShowsEveryMenuOption is the completeness contract: an option a
// customer cannot see is an option they cannot order. The test reads the MENU,
// not a hand-written list, so adding an item without rendering it fails here.
func TestCatalogueShowsEveryMenuOption(t *testing.T) {
	cat := BuildCatalogue()
	var buf bytes.Buffer
	if err := MenuCatalogue(cat).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()
	for _, group := range [][]Choice{cat.Drinks, cat.Sizes, cat.Milks, cat.Extras} {
		for _, c := range group {
			if !strings.Contains(html, c.Label) {
				t.Errorf("%q is on the menu but not in the catalogue", c.Label)
			}
			if !strings.Contains(html, `value="`+c.ID+`"`) {
				t.Errorf("%q has no selectable input", c.ID)
			}
		}
	}
}

// TestCatalogueCarriesPrices pins the "how much is it" answer: the price lives
// with the option, so the menu answers without a second screen.
func TestCatalogueCarriesPrices(t *testing.T) {
	cat := BuildCatalogue()
	for _, c := range cat.Drinks {
		if c.Price == "" {
			t.Errorf("drink %q has no price", c.ID)
		}
	}
	for _, c := range cat.Sizes {
		if c.Price == "" {
			t.Errorf("size %q has no price note", c.ID)
		}
	}
	if got := delta(0); got != "included" {
		t.Errorf("zero delta = %q, want \"included\"", got)
	}
	if got := delta(60); got != "+$0.60" {
		t.Errorf("positive delta = %q, want +$0.60", got)
	}
	if got := delta(-50); got != "-$0.50" {
		t.Errorf("negative delta = %q, want -$0.50", got)
	}
}
