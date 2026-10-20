// SCOPE:plugin — stream retention bounds tests.
package nats_test

import (
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/nats"
	natswire "github.com/calionauta/gogogo/internal/natswire"
)

// Retention is a correctness property here, not tuning: an unbounded stream
// grows forever for readers that never replay (the database is the truth).
// These tests pin the bound so a future edit cannot silently drop it.

func streamLimits(t *testing.T, js nats.JetStreamLike, name string, maxAge time.Duration, maxBytes int64) {
	t.Helper()
	info, err := js.StreamInfo(name)
	if err != nil {
		t.Fatalf("StreamInfo %s: %v", name, err)
	}
	if info.Config.MaxAge != maxAge {
		t.Errorf("%s MaxAge = %v, want %v", name, info.Config.MaxAge, maxAge)
	}
	if info.Config.MaxBytes != maxBytes {
		t.Errorf("%s MaxBytes = %v, want %v", name, info.Config.MaxBytes, maxBytes)
	}
}

func TestCrudStreamIsBounded(t *testing.T) {
	h, err := nats.StartEmbedded(t.TempDir())
	if err != nil {
		t.Fatalf("StartEmbedded: %v", err)
	}
	t.Cleanup(h.Close)
	js := h.JS
	if js == nil {
		t.Fatal("JetStream not available after StartEmbedded")
	}
	if natswire.NewCrudPublisher(js, "test") == nil {
		t.Fatal("publisher is nil with JetStream up")
	}
	streamLimits(t, js, "APP_CRUD", 7*24*time.Hour, 512<<20)
}
