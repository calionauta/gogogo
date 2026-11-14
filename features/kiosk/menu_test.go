// The kiosk parser's contract, one case per behavior a customer can produce.
//
// These are the tests `recipe check` looks for, and the ones a browser smoke
// cannot replace: the value of a deterministic parser is that every utterance
// shape is a table row.
package kiosk

import (
	"slices"
	"testing"
)

func TestParseSettlesTheCommonOrder(t *testing.T) {
	cases := []struct {
		name string
		text string
		want Order
	}{
		{"plain drink and size", "a large latte", Order{Drink: "latte", Size: "large"}},
		{"size first", "large cappuccino", Order{Drink: "cappuccino", Size: "large"}},
		{
			"milk and extra", "medium flat white with oat milk and an extra shot",
			Order{Drink: "flat_white", Size: "medium", Milk: "oat", Extras: []string{"extra_shot"}},
		},
		{"multi-word alias, upper case", "CAFFE LATTE SMALL", Order{Drink: "latte", Size: "small"}},
		{
			"lactose free", "large mocha with lactose free milk",
			Order{Drink: "mocha", Size: "large", Milk: "lactose_free"},
		},
		{
			"two extras", "medium tea with cinnamon and syrup",
			Order{Drink: "tea", Size: "medium", Extras: []string{"syrup", "cinnamon"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse(tc.text)
			if !got.Complete() {
				t.Fatalf("Parse(%q) = %+v, want a complete order", tc.text, got)
			}
			if got.Order.Drink != tc.want.Drink || got.Order.Size != tc.want.Size || got.Order.Milk != tc.want.Milk {
				t.Fatalf("Parse(%q) = %+v, want %+v", tc.text, got.Order, tc.want)
			}
			if !slices.Equal(got.Order.Extras, tc.want.Extras) {
				t.Fatalf("Parse(%q) extras = %v, want %v", tc.text, got.Order.Extras, tc.want.Extras)
			}
		})
	}
}

func TestParseReportsWhatIsMissing(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{"a latte", []string{"size"}},
		{"large", []string{"drink"}},
		{"", []string{"drink"}},
		{"an iced kiosk with caramel", []string{"drink"}},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			got := Parse(tc.text)
			if !slices.Equal(got.Missing, tc.want) {
				t.Fatalf("Parse(%q).Missing = %v, want %v", tc.text, got.Missing, tc.want)
			}
		})
	}
}

// TestParseRefusesToGuessBetweenTwoDrinks is the fail-fast half: "a latte and a
// mocha" is not a default, it is a question.
func TestParseRefusesToGuessBetweenTwoDrinks(t *testing.T) {
	got := Parse("a large latte and a mocha")
	if !got.Ambiguous {
		t.Fatalf("Parse(two drinks) = %+v, want Ambiguous", got)
	}
	if got.Complete() {
		t.Fatal("an ambiguous parse must not be confirmable")
	}
	if got.Order.Drink != "" {
		t.Fatalf("an ambiguous parse kept a drink: %q", got.Order.Drink)
	}
}

func TestPriceCentsAddsSizeAndEachExtraOnce(t *testing.T) {
	order := Order{Drink: "latte", Size: "large", Extras: []string{"extra_shot", "cinnamon"}}
	// 430 base + 60 large + 80 shot + 0 cinnamon
	if got := PriceCents(order); got != 570 {
		t.Fatalf("PriceCents(%+v) = %d, want 570", order, got)
	}
	if got := PriceCents(Order{Drink: "espresso", Size: "small"}); got != 300 {
		t.Fatalf("small espresso = %d, want 300", got)
	}
	// An unknown drink prices at zero rather than inventing a number; Validate
	// is what refuses it.
	if got := PriceCents(Order{Drink: "unicorn", Size: "large"}); got != 0 {
		t.Fatalf("unknown drink priced at %d, want 0", got)
	}
}

func TestValidateRefusesIDsOutsideTheMenu(t *testing.T) {
	onMenu := Order{Drink: "latte", Size: "large", Milk: "oat", Extras: []string{"syrup"}}
	if problems := Validate(onMenu); len(problems) != 0 {
		t.Fatalf("a menu order must validate, got %v", problems)
	}
	cases := []Order{
		{Drink: "unicorn", Size: "large"},
		{Drink: "latte", Size: "huge"},
		{Drink: "latte", Size: "large", Milk: "goat"},
		{Drink: "latte", Size: "large", Extras: []string{"gold_leaf"}},
	}
	for _, order := range cases {
		if len(Validate(order)) == 0 {
			t.Fatalf("Validate(%+v) accepted an ID outside the menu", order)
		}
	}
}

// TestSerializeDistinguishesThePreview is the counter signal's basis: a confirmed
// order that differs from what was previewed is a repair.
func TestSerializeDistinguishesThePreview(t *testing.T) {
	a := Parse("large latte")
	b := Parse("small latte")
	if Serialize(a.Order) == Serialize(b.Order) {
		t.Fatal("two different orders serialized the same")
	}
	first := Serialize(a.Order)
	if first != Serialize(a.Order) {
		t.Fatal("serialization is not stable for one order")
	}
}

// TestPhrasesComeFromTheMenu pins the contextual bias to the vocabulary, so the
// recognizer cannot drift from what the parser accepts.
func TestPhrasesComeFromTheMenu(t *testing.T) {
	phrases := Phrases()
	if len(phrases) != len(Drinks) {
		t.Fatalf("Phrases() has %d entries, want %d", len(phrases), len(Drinks))
	}
	for _, d := range Drinks {
		if !slices.Contains(phrases, d.Label) {
			t.Errorf("Phrases() is missing %q", d.Label)
		}
	}
}
