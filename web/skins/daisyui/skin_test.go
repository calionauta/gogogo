package daisyui

import (
	"context"
	"strings"
	"testing"
)

// TestAssetsIncludeThemeJS pins the dark/light toggle wiring: the navbar
// button is dead without /static/theme.js (no delegated click listener),
// and the default skin once omitted it while basecoat included it.
func TestAssetsIncludeThemeJS(t *testing.T) {
	var sb strings.Builder
	if err := assets().Render(context.Background(), &sb); err != nil {
		t.Fatalf("render daisyui assets: %v", err)
	}
	if !strings.Contains(sb.String(), `/static/theme.js`) {
		t.Errorf("daisyui assets missing theme.js toggle script:\n%s", sb.String())
	}
}
