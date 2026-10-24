// SCOPE:core — StepList legend contract: the state vocabulary is visible
// in the list itself, so no panel has to explain what a color means.
package ui

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestStepListCarriesItsLegend pins the three-state vocabulary: the legend
// reuses the exact marker classes the steps light up (is-active, is-done,
// bare for pending), so the dots cannot drift from what they explain.
func TestStepListCarriesItsLegend(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	steps := []Step{{Label: "Only", Done: "$done", Active: "$active"}}
	if err := StepList(steps).Render(context.Background(), &buf); err != nil {
		t.Fatalf("StepList: %v", err)
	}
	html := buf.String()
	for _, want := range []string{
		`app-steps-legend`,
		`app-steps-legend-item"><span class="dot"></span>pending`,
		`app-steps-legend-item is-active"><span class="dot"></span>working`,
		`app-steps-legend-item is-done"><span class="dot"></span>done`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("StepList legend missing %q", want)
		}
	}
}
