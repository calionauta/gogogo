package components

import (
	"fmt"
	"strconv"

	"github.com/calionauta/gogogo/features/kiosk"
)

// Signal names the catalogue writes. They are the preview's own signals, so a
// tap and a spoken order land in the same place.
const (
	SignalDrink  = "kioskDrink"
	SignalSize   = "kioskSize"
	SignalMilk   = "kioskMilk"
	SignalExtras = "kioskExtras"
)

// BuildCatalogue projects the menu into the rows the page renders. It is a pure
// function of the menu data, so a new drink reaches the catalogue, the parser
// and the recognizer's phrase list with one edit.
func BuildCatalogue() Catalogue {
	cat := Catalogue{ExtrasSignal: SignalExtras}
	for _, d := range kiosk.Drinks {
		cat.Drinks = append(cat.Drinks, Choice{
			ID: d.ID, Label: d.Label, Price: price(d.BaseCents),
			Signal: SignalDrink, Value: d.Label,
		})
	}
	for _, s := range kiosk.Sizes {
		cat.Sizes = append(cat.Sizes, Choice{
			ID: s.ID, Label: s.Label, Price: delta(s.DeltaCents),
			Signal: SignalSize, Value: s.Label,
		})
	}
	for _, m := range kiosk.Milks {
		cat.Milks = append(cat.Milks, Choice{
			ID: m.ID, Label: m.Label, Price: "",
			Signal: SignalMilk, Value: m.Label,
		})
	}
	for _, e := range kiosk.Extras {
		cat.Extras = append(cat.Extras, Choice{
			ID: e.ID, Label: e.Label, Price: delta(e.Cents),
			Signal: SignalExtras, Value: e.Label,
		})
	}
	return cat
}

// price formats an absolute price ("$4.30").
func price(cents int) string {
	if cents < 0 {
		cents = 0
	}
	return fmt.Sprintf("$%d.%02d", cents/100, cents%100)
}

// delta formats an adjustment the way a menu prints it: "included" for zero,
// "+$0.60" and "-$0.50" otherwise.
func delta(cents int) string {
	switch {
	case cents == 0:
		return "included"
	case cents > 0:
		return "+" + price(cents)
	default:
		return "-" + price(-cents)
	}
}

// CentsLabel is the raw number as a string, for aria text and tests.
func CentsLabel(cents int) string { return strconv.Itoa(cents) }
