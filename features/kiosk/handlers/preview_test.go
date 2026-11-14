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

// TestEngineRunsOnlyWhenItCanHelp is the latency contract. The engine costs
// about a second per question, so it must not run while somebody is still
// composing: a complete parse needs nothing, and "a big" is not a sentence yet.
func TestEngineRunsOnlyWhenItCanHelp(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"complete parse", "large latte", false},
		{"still composing", "a big", false},
		{"two words", "expresso milk", false},
		{"a request the menu cannot settle", "something cold with oat milk", true},
		{"mixed language", "um cafe grande com leite", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed := kiosk.Parse(tc.text)
			if got := engineWorthAsking(parsed, tc.text); got != tc.want {
				t.Fatalf("engineWorthAsking(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

// TestAmbiguousParseSkipsTheEngine: two drinks matched, and asking a model which
// one the CUSTOMER meant is the guess the parser refused to make.
func TestAmbiguousParseSkipsTheEngine(t *testing.T) {
	if engineWorthAsking(kiosk.Parse("latte mocha"), "latte mocha") {
		t.Error("the engine was asked about an ambiguous order")
	}
}

// TestTappedChoiceWinsOverText is the merge rule: the customer pointed at a
// value, so a stale one from an earlier sentence must not survive.
func TestTappedChoiceWinsOverText(t *testing.T) {
	parsed := kiosk.Parse("large latte with whole milk")
	got := kiosk.ApplyChoices(parsed, kiosk.Choices{Milks: []string{"oat"}})
	if got.Order.Milk != "oat" {
		t.Errorf("milk = %q, want the tapped oat", got.Order.Milk)
	}
	// The fields NOT tapped keep what the text said.
	if got.Order.Drink != "latte" || got.Order.Size != "large" {
		t.Errorf("an untouched field was lost: %+v", got.Order)
	}
}

// TestEmptyChoiceDoesNotClear: "did not tap" is not "clear it".
func TestEmptyChoiceDoesNotClear(t *testing.T) {
	parsed := kiosk.Parse("large latte")
	got := kiosk.ApplyChoices(parsed, kiosk.Choices{})
	if got.Order.Drink != "latte" || got.Order.Size != "large" {
		t.Errorf("an empty choice cleared the order: %+v", got.Order)
	}
}

// TestOffMenuChoiceIsRefused: the catalogue is a form, and a form can be built
// by hand, so an ID the counter does not sell is discarded.
func TestOffMenuChoiceIsRefused(t *testing.T) {
	got := kiosk.ApplyChoices(kiosk.Parse(""), kiosk.Choices{
		Drink: "protein_shake", Size: "gigantic", Extras: []string{"gold_leaf"},
	})
	if got.Order.Drink != "" || got.Order.Size != "" || len(got.Order.Extras) != 0 {
		t.Errorf("off-menu ids were accepted: %+v", got.Order)
	}
}

// TestExtrasAccumulate covers the one multi-select field.
func TestExtrasAccumulate(t *testing.T) {
	got := kiosk.ApplyChoices(kiosk.Parse("large latte"), kiosk.Choices{
		Extras: []string{"extra_shot", "cinnamon", "extra_shot"},
	})
	if len(got.Order.Extras) != 2 {
		t.Fatalf("extras = %v, want two (the duplicate dropped)", got.Order.Extras)
	}
}
