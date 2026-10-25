// SCOPE:core — The vendored Cally bundle's version is pinned by test.
package resources

import (
	"strings"
	"testing"
)

// pinnedCally is the Cally version this repo vendors for the Todo date filter.
// DaisyUI only styles the calendar (`class="cally"`); Cally is the web component
// that defines <calendar-date>. Bump it in the same commit that replaces
// web/resources/static/cally.js.
const pinnedCally = "0.9.2"

// TestCallyVersionIsPinned keeps the asset and the constant in step: the
// bundle's own banner is the source of truth, so replacing the file without the
// version (or the reverse) is a red build.
func TestCallyVersionIsPinned(t *testing.T) {
	t.Parallel()
	raw, err := StaticFS().Open("cally.js")
	if err != nil {
		t.Fatalf("open embedded cally.js: %v", err)
	}
	defer func() { _ = raw.Close() }()
	buf := make([]byte, 96)
	n, _ := raw.Read(buf)
	banner := string(buf[:n])
	if !strings.Contains(banner, "Cally v"+pinnedCally) {
		t.Errorf("vendored Cally is not %s:\n banner: %q\nUpdate pinnedCally together with the file.",
			pinnedCally, banner)
	}
}

// TestCallyDefinesTheCalendarElement pins what the Todo page depends on. A
// bundle that does not register <calendar-date> would leave the filter's
// element inert — the exact failure that shipped when the loader was dropped.
func TestCallyDefinesTheCalendarElement(t *testing.T) {
	t.Parallel()
	data, err := readStatic("cally.js")
	if err != nil {
		t.Fatalf("read cally.js: %v", err)
	}
	for _, want := range []string{`"calendar-date"`, `"calendar-month"`} {
		if !strings.Contains(data, want) {
			t.Errorf("cally.js no longer defines %s — the date filter would not render", want)
		}
	}
}
