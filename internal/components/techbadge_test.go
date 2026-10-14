// SCOPE:core — TechBadge render test.
package components_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/components"
)

// TestTechBadgeListsStack pins the shared disclosure contract: every
// feature page names the stack it demonstrates in one visual language
// (labeled chips under the subtitle), so visitors learn the architecture
// from any page instead of guessing which demo exercises what.
func TestTechBadgeListsStack(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	err := components.TechBadge([]string{"JetStream realtime", "SSE Hub"}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("TechBadge: %v", err)
	}
	for _, want := range []string{"Demonstrates:", "JetStream realtime", "SSE Hub"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("TechBadge missing %q", want)
		}
	}
	// The demo-only handle: product trims grep for data-demo to strip
	// every showcase element (see the TechBadge doc comment). If this
	// attribute goes missing, that trim silently keeps the badge.
	if !strings.Contains(buf.String(), `data-demo="tech-stack"`) {
		t.Error("TechBadge missing data-demo=\"tech-stack\" handle")
	}
}
