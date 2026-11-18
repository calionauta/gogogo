package queue

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/calionauta/gogogo/config"
)

// TestWorkerExtendsLeaseWhileHandlerRuns is the regression guard for the
// visibility-timeout trap.
//
// goqite re-delivers a message whose visibility timeout lapsed, and NOTHING in
// this package ever called Extend. That forced queueVisibilityTimeout to cover
// the slowest legitimate job (10 minutes), which in turn meant a worker that
// CRASHED held its message invisible for 10 minutes before anyone could pick it
// up. One constant had to be both long enough for the slowest job and short
// enough to recover fast; it cannot be both.
//
// With a heartbeat the lease is short AND a live job is never re-delivered.
// This test drives a handler that outlives the lease and asserts the message
// was extended at least once, i.e. the extension really happens on the
// production path rather than existing as an unused helper.
func TestWorkerExtendsLeaseWhileHandlerRuns(t *testing.T) {
	q, err := New(&config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Close before the pool: Close waits for in-flight Enqueue/Delete, and the
	// pool holds those locks (see goqite.go's two-tier rule).
	t.Cleanup(q.Close)

	var beats atomic.Int64
	q.extended = func() { beats.Add(1) }

	started := make(chan struct{})
	release := make(chan struct{})
	q.Registry().Register("slow", func(context.Context, *SSEHub, Job) error {
		close(started)
		<-release
		return nil
	})

	pool := NewWorkerPool(q, q.Hub(), q.Registry(), 1)
	pool.beatInterval = 20 * time.Millisecond
	pool.Start()
	t.Cleanup(pool.Stop)

	body := mustMarshal(t, Job{Type: "slow", Payload: json.RawMessage(`{}`)})
	if err := q.Enqueue(context.Background(), body); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("handler never started")
	}

	// Let at least two heartbeat intervals pass while the handler is blocked.
	deadline := time.After(2 * time.Second)
	for beats.Load() < 2 {
		select {
		case <-deadline:
			close(release)
			t.Fatalf("lease extended %d times while the handler ran — the visibility "+
				"timeout is now %v and nothing keeps a live job hidden, so a handler slower "+
				"than the lease is re-delivered and executed twice",
				beats.Load(), queueVisibilityTimeout)
		case <-time.After(5 * time.Millisecond):
		}
	}

	close(release)
}
