// SCOPE:core — PbRealtimeResync render test.
package components_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/components"
)

// TestPbRealtimeResyncWiring pins the shared resync contract every list
// page relies on: the button carries its id + topic + fragment action,
// the script finds the button through data-resync-of (no second copy of
// the id to drift), subscribes to /api/realtime, and resyncs on connect
// and on tab-visibility return. Rendered twice to prove the ids, urls
// and topics are parameters, not baked-in constants.
func TestPbRealtimeResyncWiring(t *testing.T) {
	t.Parallel()
	render := func(id, frag, topic string) string {
		t.Helper()
		var buf bytes.Buffer
		if err := components.PbRealtimeResync(id, frag, topic).Render(context.Background(), &buf); err != nil {
			t.Fatalf("PbRealtimeResync(%q): %v", id, err)
		}
		return buf.String()
	}

	notes := render("notes-realtime-resync", "'/api/notes/fragment'", "notes")
	for _, want := range []string{
		`id="notes-realtime-resync"`,
		`data-topic="notes"`,
		`data-pb-resync="1"`,
		`data-on:click`,
		`/api/notes/fragment`,
		`EventSource('/api/realtime`,
		`PB_CONNECT`,
		`visibilitychange`,
	} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes render missing %q", want)
		}
	}
	if strings.Contains(notes, "})();") {
		t.Errorf("notes render contains orphan })(); — SyntaxError kills the whole module")
	}

	wb := render("wb-realtime-resync", "'/api/whiteboards/fragment'", "whiteboards")
	for _, want := range []string{
		`id="wb-realtime-resync"`,
		`data-topic="whiteboards"`,
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
