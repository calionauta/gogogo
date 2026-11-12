// The ratio's display rule: counts first, percentage second, and an em dash when
// nothing was eligible.
package components

import "testing"

func TestOutcomeRatio(t *testing.T) {
	cases := []struct {
		name string
		in   Outcome
		want string
	}{
		{"all succeeded", Outcome{Success: 2, Total: 2}, "2/2 · 100%"},
		{"one in three", Outcome{Success: 1, Total: 3}, "1/3 · 33%"},
		{"none succeeded", Outcome{Success: 0, Total: 4}, "0/4 · 0%"},
		// The case the pair shape exists for: no denominator is not 0%.
		{"nothing eligible", Outcome{Success: 0, Total: 0}, "—"},
		{"success above total still renders the counts", Outcome{Success: 3, Total: 2}, "3/2 · 150%"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.Ratio(); got != tc.want {
				t.Fatalf("Ratio() = %q, want %q", got, tc.want)
			}
		})
	}
}
