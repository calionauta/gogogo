// The menu as a catalogue: what the counter sells, rendered as choices.
//
// WHY A CATALOGUE AND NOT ONLY THE FREE-TEXT FIELD. Dictation is one way to
// order, not the only one. A noisy room, a customer who does not know what
// "oat" is called, or a kiosk with no microphone all need the menu visible —
// and a customer who cannot see the options cannot ask for one. The catalogue
// is the same data the parser matches against, rendered as DaisyUI choices, so
// tapping and saying produce the SAME order: a choice sets the signal the
// preview already reads, and the preview stays the one place that explains the
// order.
//
// The price on each option is the real one (base, size delta, extra cents), so
// the catalogue answers "how much is a mocha" without a second source of truth.
package components

// Choice is one selectable option in the catalogue.
type Choice struct {
	// ID is the menu ID the order stores.
	ID string
	// Label is what the customer reads.
	Label string
	// Price is the option's effect on the total, already formatted ("$4.30",
	// "+$0.60", "included"). Empty means "no price to show".
	Price string
	// Signal is the Datastar signal this option selects, e.g. "kioskDrink".
	Signal string
	// Value is the label the preview shows once chosen.
	Value string
}

// Catalogue is the whole menu, ready to render.
type Catalogue struct {
	Drinks []Choice
	Sizes  []Choice
	Milks  []Choice
	Extras []Choice
	// ExtrasSignal is where the extra TAPS accumulate. Extras are the one
	// multi-select group, so they are read and written as a list.
	ExtrasSignal string
}
