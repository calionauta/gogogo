// SCOPE:feature — whiteboard doc-page navigation affordance.
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
	if err := components.Board("a@b.c", "user", meta, "dev", "", false, false).
		Render(context.Background(), &buf); err != nil {
		t.Fatalf("Board: %v", err)
	}
	html := buf.String()
	for _, want := range []string{`href="/whiteboard"`, "Boards", `data-doc-id="b0ard1"`} {
		if !strings.Contains(html, want) {
			t.Errorf("board page missing %q", want)
		}
	}
}
