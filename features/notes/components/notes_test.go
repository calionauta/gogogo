// SCOPE:feature — notes doc-page navigation affordance.
package components_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/notes/components"
)

// TestNotesPageBreadcrumbsIndex pins the way out of a specific note: a
// breadcrumb (Notes / <doc>) plus a sticky-header back link, so the way
// back to the index never depends on browser chrome.
func TestNotesPageBreadcrumbsIndex(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	meta := components.NoteMeta{DocID: "n0te1", Author: "a@b.c", IsMine: true, CanDelete: true}
	if err := components.NotesPage("a@b.c", "user", meta, "hello", 7, "dev", "").
		Render(context.Background(), &buf); err != nil {
		t.Fatalf("NotesPage: %v", err)
	}
	html := buf.String()
	for _, want := range []string{`href="/notes"`, "Notes", "n0te1", `aria-label="Breadcrumb"`, `data-doc-id="n0te1"`} {
		if !strings.Contains(html, want) {
			t.Errorf("note page missing %q", want)
		}
	}
}
