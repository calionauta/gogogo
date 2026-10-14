// SCOPE:feature — review page template tests.
package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestReviewIndexKeepsRunButtonWhenLLMDown is the red-proof for the
// "run review never enables" report: without a configured LLM the page
// hid the whole form, so users saw no button at all. The form stays
// visible with a disabled button carrying the reason, plus the shared
// stack disclosure.
func TestReviewIndexKeepsRunButtonWhenLLMDown(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := ReviewIndex("a@b.c", "dev", "", Signals{}).Render(context.Background(), &buf); err != nil {
		t.Fatalf("ReviewIndex(disabled): %v", err)
	}
	body := buf.String()
	if !strings.Contains(body, "Run review") {
		t.Fatal("LLM-down review page has no Run review button (form hidden)")
	}
	if !strings.Contains(body, "disabled") {
		t.Fatal("LLM-down review page must render the button disabled with a reason")
	}
	if !strings.Contains(body, "GOAI_API_KEY") {
		t.Fatal("LLM-down review page must say what to do (GOAI_API_KEY)")
	}
	if !strings.Contains(body, "Demonstrates:") {
		t.Fatal("review page missing the TechBadge stack disclosure")
	}
}

// TestReviewIndexEnablesRunButtonWhenLLMUp pins the live path: with a
// configured LLM the button gates on $review_pending only (never
// statically disabled), so one click starts the run.
func TestReviewIndexEnablesRunButtonWhenLLMUp(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := ReviewIndex("a@b.c", "dev", "",
		Signals{ReviewEnabled: true}).Render(context.Background(), &buf); err != nil {
		t.Fatalf("ReviewIndex(enabled): %v", err)
	}
	body := buf.String()
	if !strings.Contains(body, `data-attr:disabled="$review_pending"`) {
		t.Fatal("enabled review button must gate on $review_pending")
	}
	if !strings.Contains(body, "Demonstrates:") {
		t.Fatal("review page missing the TechBadge stack disclosure")
	}
}
