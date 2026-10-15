// SCOPE:core — TechStack render test.
package components_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/components"
)

// TestTechStackListsStack pins the shared disclosure contract: every
// feature page names the stack it demonstrates in one visual language
// (legible pill tags + one-line role notes), so visitors learn the
// architecture from any page instead of guessing which demo exercises
// what.
func TestTechStackListsStack(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	err := components.TechStack([]components.TechItem{
		{Name: "JetStream realtime", Note: "CRUD ops stream across instances"},
		{Name: "SSE Hub", Note: "every open tab re-lists live"},
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("TechStack: %v", err)
	}
	for _, want := range []string{
		"JetStream realtime", "SSE Hub",
		"CRUD ops stream across instances", "every open tab re-lists live",
		"What each layer does here",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("TechStack missing %q", want)
		}
	}
	// The demo-only handle is what lets an agent strip the template's
	// self-advertisement with one grep. Red-proof: drop the attribute and
	// this goes red. See docs/scope-taxonomy.md#demo-only-markup.
	if !strings.Contains(buf.String(), `data-demo="tech-stack"`) {
		t.Errorf("TechStack missing the data-demo handle for template removal")
	}
}
