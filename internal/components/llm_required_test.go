package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestLLMRequiredNamesTheSurfaceAndTheKnob is the contract two features now
// share: the sentence says WHAT needs a model and HOW to provide one. A notice
// that names neither is a support ticket.
func TestLLMRequiredNamesTheSurfaceAndTheKnob(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := LLMRequired("Review").Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()
	for _, want := range []string{"Review", "GOAI_API_KEY", "alert-warning", `role="status"`} {
		if !strings.Contains(html, want) {
			t.Errorf("notice is missing %q:\n%s", want, html)
		}
	}
}

// TestLLMRequiredIsNotFeatureSpecific keeps the shared leaf neutral: a feature
// name is a parameter, never baked in, or the second caller would have to copy
// the component to change one word.
func TestLLMRequiredIsNotFeatureSpecific(t *testing.T) {
	t.Parallel()
	for _, surface := range []string{"Review", "Chat", "Kiosk orders"} {
		var buf bytes.Buffer
		if err := LLMRequired(surface).Render(context.Background(), &buf); err != nil {
			t.Fatalf("render %s: %v", surface, err)
		}
		if !strings.Contains(buf.String(), surface) {
			t.Errorf("the notice does not name the surface %q it was given", surface)
		}
	}
}
