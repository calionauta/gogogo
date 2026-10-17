// SCOPE:plugin — a system panel that cannot be filled is dropped, never
// rendered empty.
package components

import (
	"context"
	"testing"

	"github.com/a-h/templ"
)

// TestHydrateDropsPanelsItCannotFill is the contract that keeps the app
// honest: a panel whose query fails is REMOVED from the answer. Rendering it
// empty would tell the user "you have no todos" — a claim the app has not
// earned, and one the model never made. The other components of the same
// answer survive.
//
// It uses the `test_panel` provider (installed once in TestMain as the
// always-failing one) rather than swapping the registry for the duration.
// That matters: an earlier version reinstalled `todo_list` and restored it in a
// cleanup, which raced a parallel test and left a non-validating provider
// installed — a cross-test failure whose cause was three files away. The
// registry is immutable after boot in tests exactly as in production; a test
// that needs a different behaviour asks for a DIFFERENT PANEL.
//
// The "no provider" case this test used to cover is gone by construction:
// Hydrate reads the installed registry, and the boot asserts every advertised
// panel HAS a provider (InstallPanelRegistry refuses an empty one), so a
// registered-but-unwired panel is a failed boot rather than a runtime drop.
// The failing-fill path is the one that can actually happen in production.
func TestHydrateDropsPanelsItCannotFill(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	panel := Directive{Component: failingPanelName, Props: map[string]any{"filter": "open"}}
	note := Directive{Component: "text_note", Props: map[string]any{"text": "here you go"}}

	got, err := Hydrate(ctx, []Directive{panel, note}, "owner")
	if err == nil {
		t.Error("a failing fill was not reported")
	}
	if len(got) != 1 || got[0].Component != "text_note" {
		t.Errorf("kept %+v, want only the text_note", got)
	}
	if _, hasRows := panel.Props["rows"]; hasRows {
		t.Error("Hydrate wrote through the caller's props map")
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
