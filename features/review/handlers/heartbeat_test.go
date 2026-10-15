// SCOPE:feature — SSE heartbeat + locked writer tests (red-proof).
package handlers

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestHeartbeatKeepsStreamAlive pins the stuck-button fix: during a long
// synchronous run the handler must emit SSE comments so proxies never see
// an idle stream (an idle-killed stream leaves $review_pending true with
// no terminal merge — the disabled-forever button). Red-proof: stop
// writing comments and no ": heartbeat" arrives within the window.
func TestHeartbeatKeepsStreamAlive(t *testing.T) {
	t.Parallel()
	old := heartbeatEvery
	heartbeatEvery = 50 * time.Millisecond
	defer func() { heartbeatEvery = old }()
	rec := httptest.NewRecorder()
	lw := &lockedWriter{w: rec}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := heartbeat(ctx, lw)
	defer stop()
	time.Sleep(300 * time.Millisecond)
	// Read under the writer mutex: the ticker goroutine may still be
	// mid-write when stop lands, and Body is not goroutine-safe.
	lw.mu.Lock()
	body := rec.Body.String()
	lw.mu.Unlock()
	if !strings.Contains(body, ": heartbeat") {
		t.Fatalf("expected heartbeat comments in stream body, got %q", body)
	}
}

// TestLockedWriterSerializesConcurrentWrites pins the interleave guard:
// heartbeat comments and patch events share one response from different
// goroutines — every write must land whole, never split mid-event.
func TestLockedWriterSerializesConcurrentWrites(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	lw := &lockedWriter{w: rec}
	const writers = 8
	const perWriter = 50
	var wg sync.WaitGroup
	for range writers {
		wg.Go(func() {
			for range perWriter {
				_, _ = lw.Write([]byte(": heartbeat\n\n"))
			}
		})
	}
	wg.Wait()
	body := rec.Body.String()
	if got, want := strings.Count(body, ": heartbeat\n\n"), writers*perWriter; got != want {
		t.Fatalf("heartbeat chunks = %d, want %d whole (interleaved write)", got, want)
	}
}
