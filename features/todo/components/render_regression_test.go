package components

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/todo"
)

// TestRender_TodoItemCheckboxReflectsCompleted is a regression guard for the
// inverted-checkbox bug: the demo loaded with one task, but toggling it
// surfaced several others because the checkbox state was rendered
// inverted / the create form double-submitted. The checkbox must be
// server-rendered from item.Completed so the UI matches the data on
// first paint (no client-side flip, no phantom rows).
func TestRender_TodoItemCheckboxReflectsCompleted(t *testing.T) {
	done := todo.Todo{ID: "abc123", Title: "finished", Completed: true}
	open := todo.Todo{ID: "def456", Title: "still open", Completed: false}

	var bDone, bOpen bytes.Buffer
	if err := TodoItem(done).Render(context.Background(), &bDone); err != nil {
		t.Fatalf("render done item: %v", err)
	}
	if err := TodoItem(open).Render(context.Background(), &bOpen); err != nil {
		t.Fatalf("render open item: %v", err)
	}
	if !strings.Contains(bDone.String(), "checked") {
		t.Errorf("completed todo must render a checked checkbox; got:\n%s", bDone.String())
	}
	if strings.Contains(bOpen.String(), "checked") {
		t.Errorf("open todo must NOT render a checked checkbox; got:\n%s", bOpen.String())
	}
	// The toggle posts to the item's own id — a regression that pointed
	// at a shared/hardcoded id would double-toggle or hit the wrong row.
	if !strings.Contains(bDone.String(), "/api/todos/abc123/toggle") {
		t.Errorf("done item toggle must target its own id (abc123)")
	}
	if !strings.Contains(bOpen.String(), "/api/todos/def456/toggle") {
		t.Errorf("open item toggle must target its own id (def456)")
	}
}

// TestRender_WorkflowTabFollowsSignal proves the durable-workflow UI is
// server-driven: the tab button, its container, and the empty-state hint
// render only when signals.DagNatsEnabled is true. Rendered twice (on/off)
// so both the enabled path and the trimmed/disabled path are pinned.
func TestRender_WorkflowTabFollowsSignal(t *testing.T) {
	base := todo.Signals{
		Todos:            nil,
		Filter:           "all",
		ItemCount:        0,
		ConnectedClients: 1,
		Suggestions:      []string{},
		SidebarTab:       "queue",
	}
	on := base
	on.DagNatsEnabled = true
	var bOn bytes.Buffer
	if err := TodoList(on).Render(context.Background(), &bOn); err != nil {
		t.Fatalf("render list (enabled): %v", err)
	}
	htmlOn := bOn.String()
	for _, want := range []string{">Durable Workflow</button>", "Run durable workflow", "/api/onboarding/start"} {
		if !strings.Contains(htmlOn, want) {
			t.Errorf("enabled UI missing %q", want)
		}
	}

	off := base
	off.DagNatsEnabled = false
	var bOff bytes.Buffer
	if err := TodoList(off).Render(context.Background(), &bOff); err != nil {
		t.Fatalf("render list (disabled): %v", err)
	}
	htmlOff := bOff.String()
	// Match user-visible UI (button label, run button, route), not HTML
	// comments: the source comment naming the three tabs always renders.
	for _, dead := range []string{">Durable Workflow</button>", "Run durable workflow", "/api/onboarding/start"} {
		if strings.Contains(htmlOff, dead) {
			t.Errorf("disabled UI must not contain %q", dead)
		}
	}
	// The queue demo stays usable: disabling workflows must not take the
	// reference demo with it.
	if !strings.Contains(htmlOff, "Run queue + retry demo") {
		t.Errorf("disabled UI lost the queue demo button")
	}
}

// TestRender_EmptyStateHintFollowsSignal pins the empty-state hint to the
// same signal: without the engine there is no workflow to run, so the
// hint must not point at it.
func TestRender_EmptyStateHintFollowsSignal(t *testing.T) {
	var bOff bytes.Buffer
	if err := TodoListRegion(todo.Signals{}).Render(context.Background(), &bOff); err != nil {
		t.Fatalf("render region (disabled): %v", err)
	}
	if strings.Contains(bOff.String(), "Run durable workflow") {
		t.Errorf("disabled empty state must not mention the workflow")
	}
	on := todo.Signals{}
	on.DagNatsEnabled = true
	var bOn bytes.Buffer
	if err := TodoListRegion(on).Render(context.Background(), &bOn); err != nil {
		t.Fatalf("render region (enabled): %v", err)
	}
	if !strings.Contains(bOn.String(), "Run durable workflow") {
		t.Errorf("enabled empty state missing the workflow hint")
	}
}

// TestRender_QueueRetryButtonEnabled is a regression guard for the
// "suggest simulated button came disabled" bug. The Queue + Retry demo
// button must render ENABLED (no static disabled attribute) so the
// goqite + retry-go demo is usable out of the box. Only signal-driven
// disabling (e.g. while the demo runs) is acceptable. In the current UI
// the button lives on the "Queue + Retry" tab and reads
// "Run queue + retry demo".
func TestRender_QueueRetryButtonEnabled(t *testing.T) {
	signals := todo.Signals{
		Todos:            nil,
		Filter:           "all",
		ItemCount:        0,
		ConnectedClients: 1,
		Suggestions:      []string{},
		SimulatedLLM:     true,
		SidebarTab:       "queue",
	}
	var buf bytes.Buffer
	if err := TodoList(signals).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render list: %v", err)
	}
	html := buf.String()

	if !strings.Contains(html, "Run queue + retry demo") {
		t.Fatalf("Queue + retry demo button missing from rendered UI")
	}
	// The button itself must not carry a hard-coded disabled attribute;
	// only the data-attr:disabled with a *signal* expression is allowed
	// (the create form uses $loading || !$newTitle.trim(), not a literal).
	if strings.Contains(html, `disabled="disabled"`) || strings.Contains(html, `disabled disabled`) {
		t.Errorf("Queue + retry button has a static disabled attribute; it must be enabled by default")
	}
}
