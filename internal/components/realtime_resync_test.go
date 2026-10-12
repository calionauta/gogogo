// SCOPE:core — ListResync render test.
package components_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/components"
)

// TestListResyncWiring pins the shared resync contract every list page
// relies on: the button carries its id + fragment action, the script
// finds the button through the data-list-resync marker (no second copy
// of the id to drift), and resyncs on load and on tab-visibility return.
// Rendered twice to prove the ids and urls are parameters, not baked-in
// constants. There is deliberately no EventSource here: record changes
// arrive over each feature's SSE stream, never over a realtime endpoint.
func TestListResyncWiring(t *testing.T) {
	t.Parallel()
	render := func(id, frag string) string {
		t.Helper()
		var buf bytes.Buffer
		if err := components.ListResync(id, frag).Render(context.Background(), &buf); err != nil {
			t.Fatalf("ListResync(%q): %v", id, err)
		}
		return buf.String()
	}

	notes := render("notes-realtime-resync", "'/api/notes/fragment'")
	for _, want := range []string{
		`id="notes-realtime-resync"`,
		`data-list-resync="1"`,
		`data-on:click`,
		`/api/notes/fragment`,
		`visibilitychange`,
	} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes render missing %q", want)
		}
	}
	for _, dead := range []string{
		`EventSource('/api/realtime`,
		`PB_CONNECT`,
		`data-topic=`,
		`data-pb-resync=`,
	} {
		if strings.Contains(notes, dead) {
			t.Errorf("notes render still carries dead realtime wiring %q", dead)
		}
	}
	if strings.Contains(notes, "})();") {
		t.Errorf("notes render contains orphan })(); — SyntaxError kills the whole module")
	}

	wb := render("wb-realtime-resync", "'/api/whiteboards/fragment'")
	for _, want := range []string{
		`id="wb-realtime-resync"`,
		`/api/whiteboards/fragment`,
	} {
		if !strings.Contains(wb, want) {
			t.Errorf("whiteboard render missing %q", want)
		}
	}
	if strings.Contains(wb, "notes-realtime-resync") || strings.Contains(wb, "/api/notes/fragment") {
		t.Errorf("whiteboard render leaks notes wiring — parameters are not applied")
	}
}
