// SCOPE:plugin — a system panel that cannot be filled is replaced by an
// honest placeholder, never dropped and never rendered empty.
package components

import (
	"context"
	"testing"

	"github.com/a-h/templ"
)

// TestHydrateReplacesPanelsItCannotFill is the contract that keeps the app
// honest: a panel whose query fails keeps its SLOT in the answer as an
// explicit "couldn't load this" placeholder.
//
// Two wrong answers bracket this one. Rendering the panel EMPTY would tell the
// user "you have no todos" — a claim the app has not earned, and one the model
// never made. DROPPING it (what this used to do) left the answer with a hole
// where the only useful part was: the request had been understood, the model
// emitted the right component, and the user saw nothing with no way to tell
// that from an empty list. The placeholder names the failure and points at the
// page that owns the data, and the other components of the answer survive
// either way.
//
// It uses the always-failing provider installed once in TestMain rather than
// swapping the registry: the registry is immutable after boot in tests exactly
// as in production, and an earlier version that reinstalled a panel raced a
// parallel test and failed three files away.
func TestHydrateReplacesPanelsItCannotFill(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	panel := Directive{Component: failingPanelName, Props: map[string]any{"filter": "open"}}
	note := Directive{Component: "text_note", Props: map[string]any{"text": "here you go"}}

	got, err := Hydrate(ctx, []Directive{panel, note}, "owner")
	if err == nil {
		t.Error("a failing fill was not reported")
	}
	if len(got) != 2 {
		t.Fatalf("kept %d directives, want both (a failed panel keeps its slot): %+v", len(got), got)
	}
	if got[0].Component != failingPanelName {
		t.Errorf("first kept = %q, want the failing panel's own name", got[0].Component)
	}
	if _, ok := got[0].Props["unavailable"].(string); !ok {
		t.Errorf("the failed panel must carry an unavailable reason, got %+v", got[0].Props)
	}
	if _, hasRows := panel.Props["rows"]; hasRows {
		t.Error("Hydrate wrote through the caller's props map")
	}
	if got[1].Component != "text_note" {
		t.Errorf("the other components must survive, got %+v", got[1])
	}
}

// TestRenderAllReplacesAFailingPanel pins the render half of the same
// contract: a panel that cannot even render is replaced in place, while a
// non-panel failure stays fatal (a half-rendered generative answer is worse
// than an error).
func TestRenderAllReplacesAFailingPanel(t *testing.T) {
	t.Parallel()
	// Empty props: the stub provider rejects a missing filter at render time,
	// which is the unrecoverable case (hydration never ran, e.g. history).
	dirs := []Directive{{Component: failingPanelName, Props: map[string]any{}}}
	comps, err := RenderAll(dirs)
	if err != nil {
		t.Fatalf("a failing panel must not fail the whole answer: %v", err)
	}
	if len(comps) != 1 {
		t.Fatalf("got %d components, want the placeholder", len(comps))
	}

	bad := []Directive{{Component: "text_note", Props: map[string]any{}}}
	if _, err := RenderAll(bad); err == nil {
		t.Error("a non-panel failure must stay fatal")
	}
}

// TestHydratePassesNonPanelsThrough pins the other half: a generative catalog
// component is not touched, so hydration cannot corrupt an answer it has no
// business in.
func TestHydratePassesNonPanelsThrough(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	note := Directive{Component: "text_note", Props: map[string]any{"text": "hi"}}
	got, err := Hydrate(ctx, []Directive{note}, "owner")
	if err != nil {
		t.Fatalf("Hydrate: %v", err)
	}
	if len(got) != 1 || got[0].Component != "text_note" {
		t.Fatalf("got %+v, want the text_note untouched", got)
	}
	if got[0].Props["text"] != "hi" {
		t.Errorf("props were rewritten: %+v", got[0].Props)
	}
}

// failingStubPanel is the ALWAYS-FAILING provider, installed once in TestMain
// under its own name. A distinct name is what lets the drop test exist without
// swapping the registry: two providers claiming one name is exactly what
// InstallPanelRegistry rejects.
type failingStubPanel struct{}

// failingPanelName is deliberately NOT the working stub's name
// (panelNameInTest): distinct panels, so neither test can disturb the other's
// fixture.
const failingPanelName = "test_failing_panel"

func (failingStubPanel) Component() string { return failingPanelName }

func (failingStubPanel) Prompt() string { return "test_failing_panel {filter} (always fails)" }

func (failingStubPanel) Writable() []string { return []string{"filter"} }

// Render makes the stub a complete provider: a panel is name + prompt +
// writable + fill + render, and the renderer consults the registry rather
// than a per-feature table, so a provider without Render would not compile
// into the seam.
func (failingStubPanel) Render(props map[string]any) (templ.Component, error) {
	filter, _ := props["filter"].(string)
	return stubPanelView(filter, nil), nil
}

// Fill always fails, which is the point: this provider exists so the
// drop-on-failure path has a panel to fail on.
func (failingStubPanel) Fill(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	return nil, context.DeadlineExceeded
}
