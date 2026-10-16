// SCOPE:plugin — a system panel that cannot be filled is dropped, never
// rendered empty.
package components

import (
	"context"
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

	for name, lister := range map[string]TodoLister{
		"no seam":     nil,
		"query error": stubLister{err: context.DeadlineExceeded},
	} {
		got, err := Hydrate(ctx, []Directive{panel, note}, "owner", lister)
		if len(got) != 1 || got[0].Component != "text_note" {
			t.Errorf("%s: kept %+v, want only the text_note", name, got)
		}
		if lister != nil && err == nil {
			t.Errorf("%s: a failing query was not reported", name)
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

// stubLister is the seam under test: rows or an error, never a store.
type stubLister struct {
	rows []TodoRow
	err  error
}

func (s stubLister) ListFor(context.Context, string, string) ([]TodoRow, error) {
	return s.rows, s.err
}
