// The ratio's display rule: counts first, percentage second, and an em dash when
// nothing was eligible. An outcome is a Metric with kind outcome — there is no
// second type.
package components

import (
	"testing"

	"github.com/calionauta/gogogo/internal/recipe"
)

func TestOutcomeMetricRatio(t *testing.T) {
	cases := []struct {
		name string
		in   Metric
		want string
	}{
		{"all succeeded", Metric{Success: 2, Total: 2}, "2/2 · 100%"},
		{"one in three", Metric{Success: 1, Total: 3}, "1/3 · 33%"},
		{"none succeeded", Metric{Success: 0, Total: 4}, "0/4 · 0%"},
		// The case the pair shape exists for: no denominator is not 0%.
		{"nothing eligible", Metric{Success: 0, Total: 0}, "—"},
		{"success above total still renders the counts", Metric{Success: 3, Total: 2}, "3/2 · 150%"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.Ratio(); got != tc.want {
				t.Fatalf("Ratio() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMetricKindDecidesRendering(t *testing.T) {
	if !(Metric{Kind: recipe.MetricOutcome}).IsOutcome() {
		t.Fatal("kind outcome must render as a ratio")
	}
	if (Metric{Kind: recipe.MetricActivity}).IsOutcome() {
		t.Fatal("activity must render as a count")
	}
	if (Metric{}).IsOutcome() {
		t.Fatal("the zero kind is activity")
	}
}
