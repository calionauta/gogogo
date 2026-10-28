// SCOPE:core — OfflineBanner render contract.
package components_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/components"
)

// TestOfflineBannerIsDeclarative pins the shape that replaced the banner
// script: the state lives in Datastar signals, the outbox publishes window
// events, and the markup carries the owner hash — never the email. A script
// tag here would mean the banner grew a second state model again.
func TestOfflineBannerIsDeclarative(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := components.OfflineBanner(true, "Owner@Example.com").Render(context.Background(), &buf); err != nil {
		t.Fatalf("OfflineBanner: %v", err)
	}
	rendered := buf.String()

	sum := sha256.Sum256([]byte("owner@example.com"))
	wantOwner := hex.EncodeToString(sum[:])
	for _, want := range []string{
		`data-offline-sync="true"`,
		`data-class=`,
		`data-owner="` + wantOwner + `"`,
		`data-class=`,
		`data-on:gogogo:outbox__window=`,
		`data-on:gogogo:synced__window=`,
		`data-on:online__window=`,
		`data-on:offline__window=`,
		`id="offline-banner-text"`,
		`id="offline-banner-signin"`,
		`id="offline-banner-drop"`,
		`window.GogogoOutbox.setOwner(el.dataset.owner)`,
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("banner render missing %q", want)
		}
	}
	// The email must NOT be in the DOM: the queue key is the hash, and a log
	// or a screenshot should not carry the signed-in address.
	if strings.Contains(strings.ToLower(rendered), "owner@example.com") {
		t.Error("banner renders the email; only its hash belongs in the page")
	}
	// No second state model.
	if strings.Contains(rendered, "offline-banner.js") || strings.Contains(rendered, "<script") {
		t.Error("banner must be declarative: no script tag and no banner script")
	}
}

// TestOfflineBannerOnlineOnlyKeepsTheCopyHonest keeps the trimmed mode honest:
// with OFFLINE_SYNC_ENABLED=false the queue words must be gone from the copy,
// because every write posts directly and nothing is stored.
func TestOfflineBannerOnlineOnlyKeepsTheCopyHonest(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := components.OfflineBanner(false, "").Render(context.Background(), &buf); err != nil {
		t.Fatalf("OfflineBanner: %v", err)
	}
	rendered := buf.String()
	if !strings.Contains(rendered, "page cache disabled") {
		t.Error("cache-disabled copy missing from the banner text expression")
	}
	// The write path is NOT disabled by the flag: the copy must not claim that
	// requests fail, and it must not offer the sign-in pause the queue owns.
	for _, banned := range []string{"requests will fail", "retrying sync", "sign in to sync"} {
		if strings.Contains(rendered, banned) {
			t.Errorf("cache-disabled banner carries stale copy %q", banned)
		}
	}
}

// TestOutboxRetireScriptOnlyWhenDisabled pins the one remaining script: it
// retires a worker an earlier deployment installed, and must not exist when
// offline sync is on.
func TestOutboxRetireScriptOnlyWhenDisabled(t *testing.T) {
	t.Parallel()
	var on bytes.Buffer
	if err := components.OutboxRetireScript(true).Render(context.Background(), &on); err != nil {
		t.Fatalf("retire script (enabled): %v", err)
	}
	if strings.Contains(on.String(), "unregister()") {
		t.Error("a healthy install must not retire its worker")
	}
	var off bytes.Buffer
	if err := components.OutboxRetireScript(false).Render(context.Background(), &off); err != nil {
		t.Fatalf("retire script (disabled): %v", err)
	}
	for _, want := range []string{"getRegistrations()", "unregister()", "caches.delete(key)"} {
		if !strings.Contains(off.String(), want) {
			t.Errorf("disabled mode missing the retirement call %q", want)
		}
	}
}
