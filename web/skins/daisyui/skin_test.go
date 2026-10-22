package daisyui

import (
	"context"
	"strings"
	"testing"
)

// TestAssetsIncludeThemeJS pins the theme-toggle wiring: the sidebar button is
// dead without /static/theme.js (no delegated click listener). The skin's asset
// list is the only thing that can drop it, so it is asserted here rather than
// discovered in a browser.
func TestAssetsIncludeThemeJS(t *testing.T) {
	var sb strings.Builder
	if err := assets().Render(context.Background(), &sb); err != nil {
		t.Fatalf("render daisyui assets: %v", err)
	}
	if !strings.Contains(sb.String(), `/static/theme.js`) {
		t.Errorf("daisyui assets missing theme.js toggle script:\n%s", sb.String())
	}
}
