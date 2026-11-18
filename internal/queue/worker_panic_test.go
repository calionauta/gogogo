package queue

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/calionauta/gogogo/config"
)

// TestPanickingHandlerDoesNotLeakOrKillTheWorker pins two failures that shipped
// together once the lease heartbeat was added.
//
// The heartbeat runs in a goroutine and is stopped by calling the func returned
// from heartbeat(). That call was written straight-line:
//
//	stopBeat := wp.heartbeat(msg.ID)
//	wp.processMessage(ctx, msg)   // panics
//	stopBeat()                    // never runs
//
// so a single panicking handler leaked the heartbeat goroutine — which then
// ticked forever, extending the lease of a message nobody was working on, so it
// was never redelivered either. Separately, nothing recovered, so the panic
// unwound worker() and the pool permanently lost a worker; nothing restarts one.
//
// Both historical shapes are red below: a leaked heartbeat fails goleak, and a
// lost worker fails the "still drains" half.
func TestPanickingHandlerDoesNotLeakOrKillTheWorker(t *testing.T) {
	// NOT parallel: goleak asserts process-global goroutine state.
	//
	// ORDER MATTERS: defer runs before t.Cleanup, and the pool's own workers
	// are goroutines. Registering the leak check first and stopping the pool
	// second (below, as a defer) makes Stop run first — LIFO. Written the other
	// way round, goleak reports the live workers this test just asserted should
	// be alive, which reads as the very leak the test is meant to catch.
	defer goleak.VerifyNone(t,
		goleak.IgnoreCurrent(),
		goleak.IgnoreTopFunction("database/sql.(*DB).connectionOpener"),
		goleak.IgnoreTopFunction("testing.(*testState).waitParallel"),
	)

	q, err := New(&config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(q.Close)

	var boom, good atomic.Int64
	q.Registry().Register("boom", func(context.Context, *SSEHub, Job) error {
		boom.Add(1)
		panic("handler exploded")
	})
	q.Registry().Register("good", func(context.Context, *SSEHub, Job) error {
		good.Add(1)
		return nil
	})

	pool := NewWorkerPool(q, q.Hub(), q.Registry(), 1) // ONE worker: its survival is observable
	pool.beatInterval = 10 * time.Millisecond
	pool.Start()
	defer pool.Stop() // runs BEFORE the goleak defer above (LIFO)

	job := Job{Type: "boom", Payload: json.RawMessage(`{}`)}
	if err := q.Enqueue(context.Background(), mustMarshal(t, job)); err != nil {
		t.Fatalf("Enqueue boom: %v", err)
	}

	// Wait for the panic to happen at least once.
	deadline := time.After(10 * time.Second)
	for boom.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("the panicking handler never ran")
		case <-time.After(10 * time.Millisecond):
		}
	}

	// The worker must still be alive: the single-worker pool has to drain a
	// normal job after the panic. A recovered panic keeps the worker; an
	// unrecovered one retires it and this hangs.
	goodJob := Job{Type: "good", Payload: json.RawMessage(`{}`)}
	if err := q.Enqueue(context.Background(), mustMarshal(t, goodJob)); err != nil {
		t.Fatalf("Enqueue good: %v", err)
	}
	deadline = time.After(10 * time.Second)
	for good.Load() == 0 {
		select {
		case <-deadline:
			t.Fatalf("the worker never processed another job after a panicking handler "+
				"(boom ran %d times): the panic retired the worker and nothing restarts one",
				boom.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// TestPanickingHandlerStillRedeliversItsMessage pins the failure that the
// straight-line stopBeat() actually caused, which goleak cannot see: the
// heartbeat goroutine DOES exit when the pool context is cancelled, so a leak
// check stays green. What it kept doing until then was EXTEND the lease of the
// message whose handler had already panicked — so goqite never considered that
// job abandoned and never redelivered it. One bad message was stuck for the
// life of the process, with a green leak check and a green "worker survived".
//
// The lease is shrunk here (wp.lease) so "the job came back" is observable in
// well under a test's patience instead of queueVisibilityTimeout.
func TestPanickingHandlerStillRedeliversItsMessage(t *testing.T) {
	q, err := New(&config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(q.Close)

	var boom atomic.Int64
	q.Registry().Register("boom", func(context.Context, *SSEHub, Job) error {
		if boom.Add(1) == 1 {
			panic("handler exploded") // only the first delivery explodes
		}
		return nil
	})

	pool := NewWorkerPool(q, q.Hub(), q.Registry(), 1)
	pool.beatInterval = 10 * time.Millisecond
	pool.lease = 150 * time.Millisecond
	pool.Start()
	defer pool.Stop()

	job := Job{Type: "boom", Payload: json.RawMessage(`{}`)}
	if err := q.Enqueue(context.Background(), mustMarshal(t, job)); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Second delivery is the assertion: the message came back after the lease
	// lapsed. With the heartbeat still extending it, this never happens.
	deadline := time.After(15 * time.Second)
	for boom.Load() < 2 {
		select {
		case <-deadline:
			t.Fatalf("the message was delivered %d time(s): after the handler panicked its lease "+
				"kept being extended, so it was never redelivered. The job was silently stuck "+
				"for the life of the pool", boom.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// TestPanickingHandlerDoesNotDeleteItsMessage pins the other half of the
// contract: a job whose handler panicked did NOT complete, so it must not be
// deleted. Deleting it would turn a crash into silent data loss.
func TestPanickingHandlerDoesNotDeleteItsMessage(t *testing.T) {
	q, err := New(&config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(q.Close)

	var boom atomic.Int64
	q.Registry().Register("boom", func(context.Context, *SSEHub, Job) error {
		boom.Add(1)
		panic("handler exploded")
	})

	pool := NewWorkerPool(q, q.Hub(), q.Registry(), 1)
	pool.beatInterval = 10 * time.Millisecond
	pool.Start()
	t.Cleanup(pool.Stop)

	body := mustMarshal(t, Job{Type: "boom", Payload: json.RawMessage(`{}`)})
	if err := q.Enqueue(context.Background(), body); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	deadline := time.After(10 * time.Second)
	for boom.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("the panicking handler never ran")
		case <-time.After(10 * time.Millisecond):
		}
	}
	// Let the worker reach its post-handler step.
	time.Sleep(100 * time.Millisecond)

	var count int
	if err := q.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM goqite`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count == 0 {
		t.Fatal("the message was DELETED after its handler panicked: a crash became silent " +
			"data loss. A panicking job must stay queued so the lease lapses and it is retried")
	}
}
