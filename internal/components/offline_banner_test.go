// SCOPE:core — OfflineBanner render + transport-script contract test.
package components_test

import (
	"bytes"
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/components"
	"github.com/calionauta/gogogo/web/resources"
)

// TestOfflineBannerDispatchesSyncEvents pins the two halves of the offline
// contract: the markup the banner renders (the sign-in affordance and the
// per-page email the SW hashes into the queue owner) and the transport script
// that owns the SW bridge. Both are easy to delete without another test
// noticing; the browser smoke then exercises the behavior (queue, retry, 401
// pause) on top of this.
func TestOfflineBannerDispatchesSyncEvents(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := components.OfflineBanner(true, "owner@example.com").Render(context.Background(), &buf); err != nil {
		t.Fatalf("OfflineBanner: %v", err)
	}
	rendered := buf.String()
	for _, want := range []string{
		`data-offline-sync="true"`,
		`data-user-email="owner@example.com"`,
		`id="offline-banner-signin"`,
		`/static/offline-banner.js?v=`,
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("banner render missing %q", want)
		}
	}

	// The script carries the SW vocabulary. sync-auth is the 401 pause;
	// gogogo:synced is the resync after a drain; queue-lost is a user-visible
	// notice when a mutation can never be saved.
	script, err := fs.ReadFile(resources.StaticFS(), "offline-banner.js")
	if err != nil {
		t.Fatalf("read offline-banner.js: %v", err)
	}
	for _, want := range []string{
		`"sync-auth"`,
		`"queue-status"`,
		`"queue-lost"`,
		`"gogogo:queued"`,
		`"gogogo:synced"`,
	} {
		if !strings.Contains(string(script), want) {
			t.Errorf("offline-banner.js missing %q", want)
		}
	}
}
