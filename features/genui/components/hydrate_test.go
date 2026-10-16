// SCOPE:plugin — a system panel that cannot be filled is dropped, never
// rendered empty.
package components

import (
	"context"
	"maps"
	"testing"
)

// TestHydrateDropsPanelsItCannotFill is the contract that keeps the app
// honest: a panel with no lister (a trimmed build) or a failing query is
// REMOVED from the answer. Rendering it empty would tell the user "you have
// no todos" — a claim the app has not earned, and one the model never made.
// The other components of the same answer survive.
func TestHydrateDropsPanelsItCannotFill(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	panel := Directive{Component: todoListName, Props: map[string]any{"filter": "open"}}
	note := Directive{Component: "text_note", Props: map[string]any{"text": "here you go"}}

	for name, providers := range map[string][]PanelProvider{
		"no provider":     nil,
		"provider errors": {stubPanel{err: context.DeadlineExceeded}},
	} {
		got, err := Hydrate(ctx, []Directive{panel, note}, "owner", providers)
		if len(got) != 1 || got[0].Component != "text_note" {
			t.Errorf("%s: kept %+v, want only the text_note", name, got)
		}
		if providers != nil && err == nil {
			t.Errorf("%s: a failing fill was not reported", name)
		}
		if _, hasRows := panel.Props["rows"]; hasRows {
			t.Errorf("%s: Hydrate wrote through the caller's props map", name)
		}
	}

	got, err := Hydrate(ctx, []Directive{panel}, "owner", nil)
	if err != nil {
		t.Fatalf("dropping a panel is not an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("kept %+v, want the panel dropped", got)
	}
}

// stubPanel is the seam under test: rows or an error, never a store. It is
// a provider like production's, so the test exercises the registry path
// rather than a special case.
type stubPanel struct {
	rows []TodoRow
	err  error
}

func (s stubPanel) Component() string      { return todoListName }
func (s stubPanel) PromptFragment() string { return "todo_list {filter}" }

func (s stubPanel) Fill(_ context.Context, _ string, props map[string]any) (map[string]any, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make(map[string]any, len(props)+1)
	maps.Copy(out, props)
	out["rows"] = s.rows
	return out, nil
}
