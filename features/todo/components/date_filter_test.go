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

func TestCreatedFilterUsesCallyCalendarAndRowDates(t *testing.T) {
	var filter bytes.Buffer
	if err := createdFilter().Render(context.Background(), &filter); err != nil {
		t.Fatalf("render filter: %v", err)
	}
	got := filter.String()
	// The calendar is a web component: DaisyUI styles `class="cally"`, Cally
	// defines <calendar-date>. Assert the element, not just the class, because
	// the failure that shipped was the element without its loader (the popover
	// opened empty). todoHead() mounting the module is asserted below.
	for _, want := range []string{
		`id="created-filter-btn"`,
		`popover`,
		`<calendar-date`,
		`class="cally`,
		`<calendar-month`,
		`7 days`,
		`30 days`,
		`$createdAfter`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("date filter missing %q", want)
		}
	}

	var head bytes.Buffer
	if err := todoHead().Render(context.Background(), &head); err != nil {
		t.Fatalf("render head extras: %v", err)
	}
	if !strings.Contains(head.String(), "cally.js") {
		t.Errorf("todo page does not load cally.js — <calendar-date> would never upgrade")
	}
	if strings.Contains(head.String(), `type="date"`) {
		t.Errorf("the native input is still rendered next to the calendar")
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
