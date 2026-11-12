// The counter-signal rule, without a database: `corrected` is the difference
// between what the customer was shown and what they confirmed, and every branch
// of that decision is a table row here.
package service

import (
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/coffee"
)

func TestCorrectedOnlyWhenThePreviewDiverged(t *testing.T) {
	previewed := coffee.Parse("large latte")
	if !previewed.Complete() {
		t.Fatalf("fixture did not parse: %+v", previewed)
	}
	shown := coffee.Serialize(previewed.Order)

	cases := []struct {
		name      string
		previewed string
		text      string
		want      bool
	}{
		{"same order", shown, "large latte", false},
		{"size changed", shown, "small latte", true},
		{"drink changed", shown, "large mocha", true},
		{"extra added", shown, "large latte with cinnamon", true},
		{"nothing was previewed", "", "large latte", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed := coffee.Parse(tc.text)
			if !parsed.Complete() {
				t.Fatalf("fixture %q did not parse: %+v", tc.text, parsed)
			}
			if got := Corrected(tc.previewed, parsed.Order); got != tc.want {
				t.Fatalf("Corrected(%q, %q) = %v, want %v", tc.previewed, tc.text, got, tc.want)
			}
		})
	}
}

// TestCorrectedIgnoresWhatWasNeverShown is the distinct case behind the rule: an
// empty preview is not a match, it is an absence, and it must not count as a
// repair — a demo without a preview would otherwise report 100% corrected.
func TestCorrectedIgnoresWhatWasNeverShown(t *testing.T) {
	order := coffee.Parse("small espresso").Order
	if Corrected("", order) {
		t.Fatal("an empty preview counted as a correction")
	}
	required := strings.Count(coffee.Serialize(order), "|")
	if required != 3 {
		t.Fatalf("serialization shape changed: %q", coffee.Serialize(order))
	}
}
