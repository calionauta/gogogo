// SCOPE:core — TechStack render test.
package components_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/components"
)

// TestTechStackListsStack pins the shared stack-key contract: every feature
// names the stack it demonstrates in ONE visual language (a STACK caption plus
// a name-and-role row per layer), rendered by the shell from Data.Stack so no
// page hand-rolls its own.
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
		"STACK",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("TechStack missing %q", want)
		}
	}
	// The demo-only handle is what lets an agent strip the template's
	// self-advertisement with one grep. Red-proof: drop the attribute and
	// this goes red. See docs/scope-taxonomy.md#demo-only-markup.
	if !strings.Contains(buf.String(), `data-demo="stack-key"`) {
		t.Errorf("TechStack missing the data-demo handle for template removal")
	}
	// The disclosure died with the sidebar counter: the key is evidence you
	// asked for (it lives in the demo panel), not a collapsible in the chrome.
	if strings.Contains(buf.String(), "<details") {
		t.Error("TechStack must not collapse itself: the demo panel is the surface")
	}
}
