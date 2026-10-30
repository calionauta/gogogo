// SCOPE:feature — Inbox page surfaces: the panel buttons, the state panel and
// the operator-only console link.
package components_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/notifications/components"
	"github.com/calionauta/gogogo/internal/ui"
)

func renderPage(ctx context.Context, t *testing.T, demo bool, data components.PageData) string {
	t.Helper()
	var buf bytes.Buffer
	if err := components.Page("ops@example.com", "admin", "dev", "abc123", demo, data).
		Render(ctx, &buf); err != nil {
		t.Fatalf("render inbox page: %v", err)
	}
	return buf.String()
}

func samplePageData() components.PageData {
	return components.PageData{
		Wired:    true,
		Channels: []components.ChannelOption{{Channel: "email", Label: "Also send by e-mail"}},
		Deliveries: []components.DeliveryCount{
			{Channel: "in_app", Status: "sent", Count: 3},
			{Channel: "email", Status: "skipped", Count: 1},
		},
	}
}

// TestInboxOffersBothPanelSections pins the two header buttons and the seeds
// they act on. The page has a pipeline beside it (the engine, the router, the
// delivery rows), so it owes the reader both the machinery ([ State ]) and the
// technology key ([ Stack ]) — the same affordance every other module page
// carries. The panel itself is always in the DOM (the buttons only flip
// `$panelVisible`); the demo cookie decides whether it starts open.
func TestInboxOffersBothPanelSections(t *testing.T) {
	t.Parallel()
	closed := renderPage(context.Background(), t, false, samplePageData())
	for _, want := range []string{
		`data-panel-mode="state"`,
		`data-panel-mode="stack"`,
		`id="state-panel"`,
		`&#34;panelVisible&#34;:false`,
	} {
		if !strings.Contains(closed, want) {
			t.Errorf("inbox page missing %q", want)
		}
	}
	opened := renderPage(context.Background(), t, true, samplePageData())
	if !strings.Contains(opened, `&#34;panelVisible&#34;:true`) {
		t.Error("the demo cookie did not open the panel on load")
	}
}

// TestInboxStatePanelShowsDeliveryEvidence pins what the panel is FOR: the
// pipeline (engine, channels) and the per-channel delivery rows the inbox list
// deliberately hides. A panel that only repeated the list would be decoration.
func TestInboxStatePanelShowsDeliveryEvidence(t *testing.T) {
	t.Parallel()
	html := renderPage(context.Background(), t, true, samplePageData())
	for _, want := range []string{
		`id="state-panel"`,
		"Delivery pipeline",
		"wired",
		"in-app, email",
		"In-app · sent",
		"Email · skipped",
		"Turso",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("state/stack panel missing %q", want)
		}
	}
}

// TestInboxDagNatsLinkIsOperatorOnly pins the contextual link: an operator can
// open the console to see the scheduled run, while everyone else reads the same
// sentence without a link that answers 403.
func TestInboxDagNatsLinkIsOperatorOnly(t *testing.T) {
	t.Parallel()
	plain := renderPage(context.Background(), t, false, samplePageData())
	if strings.Contains(plain, `href="/dagnats/"`) {
		t.Error("a non-operator page links to the operator console")
	}
	if !strings.Contains(plain, "DagNats&#32;<code>sleep") || strings.Contains(plain, "DagNatssleep") {
		t.Errorf("reminder hint lost the space before sleep: %s", plain)
	}
	operator := renderPage(ui.WithOperator(context.Background(), true), t, false, samplePageData())
	if !strings.Contains(operator, `href="/dagnats/"`) {
		t.Error("an operator page does not link the DagNats console")
	}
}
