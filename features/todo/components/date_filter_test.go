// The date filter is display-only: the box carries a Cally calendar, the row
// carries its creation date, and data-show compares them. This test pins the
// markup contract the browser needs.
package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/features/todo"
)

func TestCreatedFilterWiresCallyAndRowDates(t *testing.T) {
	var filter bytes.Buffer
	if err := createdFilter().Render(context.Background(), &filter); err != nil {
		t.Fatalf("render filter: %v", err)
	}
	got := filter.String()
	for _, want := range []string{
		`id="created-filter-btn"`,
		`popover`,
		`calendar-date`,
		`class="cally"`,
		`$createdAfter`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("date filter missing %q", want)
		}
	}

	var row bytes.Buffer
	item := todo.Todo{ID: "1", Title: "dated", CreatedAt: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)}
	if err := TodoItem(item).Render(context.Background(), &row); err != nil {
		t.Fatalf("render item: %v", err)
	}
	if !strings.Contains(row.String(), `data-created="2026-10-21"`) {
		t.Errorf("row must carry its creation date: %s", row.String())
	}
}
