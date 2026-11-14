// The preview's "changes while you speak" contract: every field the parser
// settled is published, even when the order is not complete yet.
package handlers

import (
	"testing"

	"github.com/calionauta/gogogo/features/kiosk"
)

// TestPartialOrderShowsWhatItKnows is the reported bug: "I'd like a big drink"
// parsed size=large and then the preview left EVERY field blank, because the
// whole block was gated on a drink. The page looked frozen while the user spoke.
func TestPartialOrderShowsWhatItKnows(t *testing.T) {
	sig := previewSignals(kiosk.Parse("i'd like a big drink"))

	if sig["kioskSize"] != "Large" {
		t.Errorf("kioskSize = %v, want Large — a settled size must show", sig["kioskSize"])
	}
	// The price and the preview token WAIT for a drink: a size alone has no
	// price, and a partial token would look confirmable to the handler.
	if sig["kioskPrice"] != "" {
		t.Errorf("kioskPrice = %v, want empty without a drink", sig["kioskPrice"])
	}
	if sig["kioskPreview"] != "" {
		t.Errorf("kioskPreview = %v, want empty without a drink", sig["kioskPreview"])
	}
	if sig["kioskNeed"] == "" {
		t.Error("a missing drink must say so")
	}
}

// TestCompleteOrderShowsEverything is the positive control: the split above
// must not have broken the full case.
func TestCompleteOrderShowsEverything(t *testing.T) {
	sig := previewSignals(kiosk.Parse("a large latte with oat milk"))

	for field, want := range map[string]string{
		"kioskDrink": "Latte", "kioskSize": "Large", "kioskMilk": "Oat",
	} {
		if sig[field] != want {
			t.Errorf("%s = %v, want %q", field, sig[field], want)
		}
	}
	if sig["kioskPrice"] == "" || sig["kioskPreview"] == "" {
		t.Errorf("a complete order must be priced and confirmable: %v", sig)
	}
	if sig["kioskNeed"] != "" {
		t.Errorf("a complete order must not report a missing field: %v", sig["kioskNeed"])
	}
}

// TestCorrectionClearsStaleFields: every signal is written on every answer, so
// deleting the milk must not leave "Oat" on screen.
func TestCorrectionClearsStaleFields(t *testing.T) {
	withMilk := previewSignals(kiosk.Parse("large latte with oat milk"))
	if withMilk["kioskMilk"] != "Oat" {
		t.Fatalf("setup: kioskMilk = %v", withMilk["kioskMilk"])
	}
	without := previewSignals(kiosk.Parse("large latte"))
	if without["kioskMilk"] != "" {
		t.Errorf("kioskMilk = %v after the milk was removed", without["kioskMilk"])
	}
}
