package queue

import (
	"context"
	"testing"
	"time"

	"github.com/calionauta/gogogo/config"
)

// TestWaitCtxReturnsOnCancel pins the primitive that replaced time.Sleep in the
// worker loop: it must return as soon as the context is cancelled, not when the
// duration elapses. This is the whole reason the worker's receive-error backoff
// is not a sleep.
func TestWaitCtxReturnsOnCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	start := time.Now()
	err := waitCtx(ctx, 30*time.Second)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("waitCtx returned nil for a cancelled context")
	}
	if elapsed > time.Second {
		t.Fatalf("waitCtx waited %v on a cancelled context — it is a sleep, not a wait", elapsed)
	}
}

// TestWaitCtxWaitsWhenNotCancelled is the other half: it must still actually
// wait for the duration, or the backoff would be a spin.
func TestWaitCtxWaitsWhenNotCancelled(t *testing.T) {
	t.Parallel()

	start := time.Now()
	if err := waitCtx(context.Background(), 40*time.Millisecond); err != nil {
		t.Fatalf("waitCtx: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Fatalf("waitCtx returned after %v, expected ~40ms", elapsed)
	}
}

// TestWorkerStopIsPromptWhenQueueFails is the regression guard for the
// shutdown bug.
//
// The worker used a bare `time.Sleep(time.Second)` on the receive-error path.
// Awaiting that sleep is uncancellable: Stop() cancels the pool context and
// then blocks in wg.Wait(), so a worker sitting in the sleep held shutdown for
// up to a full second — on every worker, on every shutdown, for a queue that
// was merely failing. The wait is now a select on the pool context, so Stop()
// returns promptly no matter what the queue is doing.
//
// The test closes the underlying queue to force ReceiveAndWait to error
// repeatedly, then asserts Stop() completes well inside the old 1s window.
func TestWorkerStopIsPromptWhenQueueFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	q, err := New(&config.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pool := NewWorkerPool(q.q, q.Hub(), q.Registry(), 4)
	pool.Start()

	// Close the queue out from under the workers so every receive fails. This
	// is the state that used to send each worker into a 1-second sleep.
	q.Close()
	// Give the workers a moment to enter the error path.
	time.Sleep(50 * time.Millisecond)

	start := time.Now()
	pool.Stop()
	elapsed := time.Since(start)

	// Generous bound: the old code could hold up to 1s per worker. Anything
	// near that means the sleep is back.
	if elapsed > 700*time.Millisecond {
		t.Fatalf("Stop() took %v with a failing queue — the receive-error wait is "+
			"not cancellable (a bare time.Sleep holds shutdown)", elapsed)
	}
	t.Logf("Stop() with a failing queue returned in %v", elapsed)
}
