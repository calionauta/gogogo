// Whiteboard doc-page navigation affordance.
package components_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/whiteboard/components"
)

// TestBoardPageLinksBackToIndex pins the way out of a specific board: the
// sticky header carries a back link to the index, so the canvas toolbar
// needs no buried "Back" button of its own.
func TestBoardPageLinksBackToIndex(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	meta := components.BoardMeta{DocID: "b0ard1", DocVer: 3, Author: "a@b.c", IsMine: true, CanDelete: true}
	if err := components.Board("a@b.c", "user", meta, "dev", "", false, false, `[{"id":"seed-1","type":"rect"}]`).
		Render(context.Background(), &buf); err != nil {
		t.Fatalf("Board: %v", err)
	}
	html := buf.String()
	for _, want := range []string{
		`href="/whiteboard"`, "Boards", `data-doc-id="b0ard1"`,
		`data-shapes=`, `data-seeded-at=`, "seed-1",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("board page missing %q", want)
		}
	}
}

// TestWhiteboardListMatchesNotesListShape pins that the two index lists are the
// same shape. They are the same thing to a user — a document you open by
// clicking a row — so a whiteboard index that renders differently reads as a
// different product.
//
// This is also a REGRESSION GUARD for a real layout bug: the list used to be
// `grid` + one `ui-card ui-card-body flex flex-row` per row. `ui-card` is the
// card's outer box and `ui-card-body` the padding inside it, so stacking both
// on one element is already wrong; and `flex-row` then lost to `ui-card-body`'s
// `flex-direction: column` because Tailwind v4 emits utilities into `@layer
// utilities` while chrome.css is unlayered — unlayered CSS beats any layer
// regardless of order. The link and its delete button stacked vertically and
// the row read as a cramped centred block.
func TestWhiteboardListMatchesNotesListShape(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	boards := []components.BoardMeta{{DocID: "b0ard1", DocVer: 3, Author: "a@b.c", IsMine: true, CanDelete: true}}
	if err := components.WhiteboardListFragment(boards).Render(context.Background(), &buf); err != nil {
		t.Fatalf("WhiteboardListFragment: %v", err)
	}
	html := buf.String()

	// The notes index's shape: a menu list of rows, one card component per row.
	wantShape := []string{
		`<ul class="menu`,
		`class="flex items-center justify-between gap-2"`,
		`id="whiteboard-list"`,
	}
	for _, want := range wantShape {
		if !strings.Contains(html, want) {
			t.Errorf("whiteboard list missing %q — it must keep the notes index shape", want)
		}
	}
	// The shapes that produced the bug: `grid` on the container and a card
	// component stacked on ui-card-body for a row.
	for _, dontWant := range []string{`class="grid gap-2"`, "ui-card-body flex flex-row"} {
		if strings.Contains(html, dontWant) {
			t.Errorf("whiteboard list contains %q — ui-card-body sets flex-direction: column "+
				"and it beats Tailwind's layered flex-row, so rows stack", dontWant)
		}
	}
}
