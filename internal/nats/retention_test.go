// SCOPE:layer=infra,removal=plugin — stream/KV retention bounds tests.
package nats_test

import (
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/nats"
	"github.com/calionauta/gogogo/internal/queue"
)

// Retention is a correctness property here, not tuning: unbounded streams
// grow forever for readers that never replay (PB is the truth), and
// coordination KV without TTL accumulates crashed members. These tests
// pin the bounds so a future edit cannot silently drop them.

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

func TestTodoStreamIsBounded(t *testing.T) {
	h, err := nats.StartEmbedded(t.TempDir())
	if err != nil {
		t.Fatalf("StartEmbedded: %v", err)
	}
	t.Cleanup(h.Close)
	js := h.JS
	if js == nil {
		t.Fatal("JetStream not available after StartEmbedded")
	}
	if _, err := nats.NewJetStreamBroadcaster(js, queue.NewSSEHub()); err != nil {
		t.Fatalf("broadcaster: %v", err)
	}
	streamLimits(t, js, "TODOS", 24*time.Hour, 256<<20)
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
	if nats.NewCrudPublisher(js) == nil {
		t.Fatal("publisher is nil with JetStream up")
	}
	streamLimits(t, js, "APP_CRUD", 7*24*time.Hour, 512<<20)
}

func TestPresenceBucketIsBounded(t *testing.T) {
	h, err := nats.StartEmbedded(t.TempDir())
	if err != nil {
		t.Fatalf("StartEmbedded: %v", err)
	}
	t.Cleanup(h.Close)
	js := h.JS
	if js == nil {
		t.Fatal("JetStream not available after StartEmbedded")
	}
	if _, kvErr := nats.EnsureKeyValue("room-presence", 1, 24*time.Hour); kvErr != nil {
		// Bucket may already exist from another test in this package
		// sharing the server; limits below still apply to it.
		t.Logf("bucket exists: %v", kvErr)
	}
	info, err := js.StreamInfo("KV_room-presence")
	if err != nil {
		t.Fatalf("StreamInfo KV_room-presence: %v", err)
	}
	if info.Config.MaxMsgsPerSubject != 1 {
		t.Errorf("room-presence history = %v, want 1 (latest per member)", info.Config.MaxMsgsPerSubject)
	}
	if info.Config.MaxAge != 24*time.Hour {
		t.Errorf("room-presence MaxAge = %v, want 24h", info.Config.MaxAge)
	}
}
