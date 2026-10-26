// SCOPE:core — OfflineBanner render test.
package components_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/components"
)

// TestOfflineBannerDispatchesSyncEvents pins the SW postMessage bridge
// vocabulary the banner owns: sync-end (replay drained) must dispatch
// gogogo:synced so list pages refetch through their resync button, next
// to the existing gogogo:queued reset. Delete the dispatch and offline
// replays converge only when the live SSE event lands inside the
// reconnect window — the flake this contract closes.
func TestOfflineBannerDispatchesSyncEvents(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := components.OfflineBanner(true).Render(context.Background(), &buf); err != nil {
		t.Fatalf("OfflineBanner: %v", err)
	}
	rendered := buf.String()
	for _, want := range []string{
		`gogogo:queued`,
		`gogogo:synced`,
		// A 401 mid-queue pauses the replay and offers the fix (a link that
		// returns to this page). Both halves are easy to delete without any
		// other test noticing.
		`sync-auth`,
		`offline-banner-signin`,
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("banner render missing %q", want)
		}
	}
}
