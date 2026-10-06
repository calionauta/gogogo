package queue

import (
	"context"
	"testing"
	"time"

	"github.com/calionauta/gogogo/config"
)

// TestWaitOrStopReturnsOnCancel pins the primitive that replaced time.Sleep in
// the worker loop: it must return as soon as the pool starts shutting down, not
// when the duration elapses. This is the whole reason the receive-error backoff
// is not a sleep.
func TestWaitOrStopReturnsOnCancel(t *testing.T) {
	t.Parallel()

	wp := &WorkerPool{
		stopCh: make(chan struct{}),
	}
	wp.ctx, wp.cancel = context.WithCancel(context.Background())
	wp.cancel() // already cancelled

	timer := time.NewTimer(time.Hour)
	defer timer.Stop()

	start := time.Now()
	stopping := wp.waitOrStop(timer, 30*time.Second)
	elapsed := time.Since(start)

	if !stopping {
		t.Fatal("waitOrStop returned false for a cancelled pool — the wait would outlive shutdown")
	}
	if elapsed > time.Second {
		t.Fatalf("waitOrStop took %v on a cancelled pool — it is a sleep, not a wait", elapsed)
	}
}

// TestWaitOrStopWaitsWhenRunning is the other half: with a live pool it must
// actually wait the duration, or the backoff would be a spin.
func TestWaitOrStopWaitsWhenRunning(t *testing.T) {
	t.Parallel()

	wp := &WorkerPool{stopCh: make(chan struct{})}
	wp.ctx, wp.cancel = context.WithCancel(context.Background())
	defer wp.cancel()

	timer := time.NewTimer(time.Hour)
	defer timer.Stop()

	start := time.Now()
	if stopping := wp.waitOrStop(timer, 40*time.Millisecond); stopping {
		t.Fatal("waitOrStop reported stopping on a live pool")
	}
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Fatalf("waitOrStop returned after %v, expected ~40ms", elapsed)
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

// TestQueueCloseStopsWorkers is the regression guard for a shutdown path that
// was unreachable in production.
//
// internal/server/boot.go called `workersLocal := q.StartWorkers(); _ = workersLocal`
// — the pool was assigned to a discarded local, so WorkerPool.Stop() could
// never be called. q.Close() only nilled the queue handle and closed the DB,
// leaving every worker goroutine looping against a dead database. The fix
// stores the pool on the Queue, so Close() always stops it; this asserts the
// observable contract (Close returns, and it returns promptly even with a live
// worker loop).
func TestQueueCloseStopsWorkers(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	q, err := New(&config.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pool := q.StartWorkers()
	if pool == nil {
		t.Fatal("StartWorkers returned nil")
	}
	// Close must own the pool: a caller that discards the return value (as
	// boot.go did) still gets a stopped pool.
	if q.workers == nil {
		t.Fatal("StartWorkers did not record the pool on the Queue — Close cannot stop it")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		q.Close()
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("q.Close() did not return within 5s — workers are not being stopped")
	}

	if q.workers != nil {
		t.Error("Close left the worker pool set")
	}
	// Stopping twice must stay safe (Close can be reached from more than one
	// deferred path).
	q.Close()
}
