// Queue handlers + retry + workers
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"maragu.dev/goqite"
)

const (
	// idleTick paces empty receives so a quiet queue is not polled in a tight
	// loop. Named rather than inlined because it is a timing budget the pool
	// pays per idle worker.
	idleTick = 200 * time.Millisecond

	// receiveErrorBackoff spaces out receive-error retries: a persistently
	// failing queue (locked database, disk error) must not be hammered in a hot
	// loop. It is a wait, not a sleep — see waitOrStop for why that distinction
	// is load-bearing.
	receiveErrorBackoff = time.Second
)

// WorkerPool drains the underlying goqite queue, dispatches each
// message to a registered Handler (looked up via the HandlerRegistry),
// wraps the handler invocation in RetryConfig.Do so transient failures
// stream SSE feedback to the user, and deletes the message after the
// handler returns (successfully or after exhausting retries).
//
// This is the production path for SSE-aware async work: handlers can
// stream progress chunks via hub.Send/Broadcast, retry policy is
// centralized in RetryConfig, and the worker pool never blocks longer
// than MaxDelay between attempts.
type WorkerPool struct {
	// q is the OWNING Queue, not a goqite handle: the pool must go through its
	// locked accessors so a concurrent Close cannot invalidate what a worker is
	// mid-call on. It used to carry a raw *goqite.Queue plus a qMu that nothing
	// ever write-locked, which made the mutex decorative.
	q        queueSource
	hub      *SSEHub
	reg      *HandlerRegistry
	retry    *RetryConfig
	count    int
	stopCh   chan struct{}
	wg       sync.WaitGroup
	ctx      context.Context //nolint:containedctx // lifecycle context for the pool, not request-scoped
	cancel   context.CancelFunc
	stopOnce sync.Once

	// beatInterval and lease override the production timing in tests. Zero means
	// the package constants. Fields rather than package vars so two pools in one
	// process cannot see each other's values.
	beatInterval time.Duration
	lease        time.Duration
}

// NewWorkerPool wires a worker pool with default retry settings
// (DefaultRetryConfig: 3 attempts, 2s→30s exponential backoff, ±20% jitter).
// Call SetRetry to override.
// queueSource is the slice of Queue the pool needs. Declared at the consumer
// (Go interfaces belong where they are used) so a test can drive the pool with
// a fake, and so the pool cannot reach for anything else on the queue.
type queueSource interface {
	receiveAndWait(ctx context.Context, timeout time.Duration) (*goqite.Message, error)
	deleteMessage(ctx context.Context, id goqite.ID) error
	extend(ctx context.Context, id goqite.ID, lease time.Duration) error
}

func NewWorkerPool(q queueSource, hub *SSEHub, reg *HandlerRegistry, count int) *WorkerPool {
	return &WorkerPool{
		q:      q,
		hub:    hub,
		reg:    reg,
		retry:  &DefaultRetryConfig,
		count:  count,
		stopCh: make(chan struct{}),
	}
}

// SetRetry overrides the default retry config. Must be called before Start.
func (wp *WorkerPool) SetRetry(cfg RetryConfig) {
	wp.retry = &cfg
}

// Start launches count workers. Each worker is a goroutine that loops:
//  1. Receive a message from the queue (with backoff on empty).
//  2. Decode the Job envelope.
//  3. Look up the handler in the registry.
//  4. Invoke the handler under retry.Do with SSE feedback.
//  5. Delete the message from the queue.
//
// Start creates a cancelable context so Stop() can interrupt an idle
// worker blocked inside ReceiveAndWait (which polls forever on a
// Background context) and let the pool shut down cleanly.
func (wp *WorkerPool) Start() {
	wp.ctx, wp.cancel = context.WithCancel(context.Background())
	for i := range wp.count {
		wp.wg.Add(1)
		go wp.worker(i)
	}
	slog.Info("queue workers started", "count", wp.count, "retry_attempts", wp.retry.Attempts)
}

// Stop drains in-flight workers (up to RetryConfig.MaxDelay between
// attempts) and blocks until all goroutines have exited. It is
// idempotent and safe to call before Start or after a prior Stop.
func (wp *WorkerPool) Stop() {
	wp.stopOnce.Do(func() {
		if wp.cancel != nil {
			wp.cancel() // unblock ReceiveAndWait on idle workers
		}
		close(wp.stopCh)
	})
	wp.wg.Wait()
	slog.Info("queue workers stopped")
}

// stopping reports whether the pool is shutting down, without blocking.
//
// Every wait in the worker loop checks this before doing anything else: a
// cancelled pool context and a closed stopCh both mean "exit now", and the
// shutdown path must never be mistaken for a queue failure (which would log an
// error and take a backoff on the way out).
func (wp *WorkerPool) stopping() bool {
	select {
	case <-wp.stopCh:
		return true
	case <-wp.ctx.Done():
		return true
	default:
		return false
	}
}

// waitOrStop blocks until d elapses or the pool starts shutting down. It
// returns true when the pool is stopping (the caller must return) and false
// when the delay completed normally.
//
// This is the replacement for a bare `time.Sleep`: that sleep is
// uncancellable, so Stop() — which cancels wp.ctx and then blocks in wg.Wait()
// — had to wait out the full delay on every worker sitting in one.
func (wp *WorkerPool) waitOrStop(t *time.Timer, d time.Duration) bool {
	t.Reset(d)
	select {
	case <-wp.stopCh:
		return true
	case <-wp.ctx.Done():
		return true
	case <-t.C:
		return false
	}
}

func (wp *WorkerPool) worker(id int) {
	defer wp.wg.Done()

	// Reusable timers: a `time.After` inside the loop would allocate a new Timer
	// on every tick (4 workers x ~1/s). Reset them instead.
	idle := time.NewTimer(idleTick)
	if !idle.Stop() {
		<-idle.C
	}
	defer idle.Stop()

	backoff := time.NewTimer(receiveErrorBackoff)
	if !backoff.Stop() {
		<-backoff.C
	}
	defer backoff.Stop()

	for {
		if wp.stopping() {
			return
		}

		msg, err := wp.q.receiveAndWait(wp.ctx, time.Second)
		if err != nil {
			if wp.stopping() || errors.Is(err, ErrQueueClosed) {
				return // a teardown, not a queue failure
			}
			slog.Warn("queue worker: receive error", "worker_id", id, "error", err)
			if wp.waitOrStop(backoff, receiveErrorBackoff) {
				return
			}
			continue
		}
		if msg == nil {
			// Idle: no message this second. Log at Debug — an Info line here is
			// one entry per worker per idle second, forever.
			slog.Debug("queue worker: idle", "worker_id", id)
			if wp.waitOrStop(idle, idleTick) {
				return
			}
			continue
		}
		slog.Debug("queue worker: received", "worker_id", id)

		if rec := wp.runLeased(msg); rec != nil {
			// The handler panicked: do NOT delete, and hand the message back.
			wp.releasePanicked(msg.ID)
			continue
		}

		if err := wp.q.deleteMessage(context.Background(), msg.ID); err != nil {
			if errors.Is(err, ErrQueueClosed) {
				return // shutdown drained the queue out from under us
			}
			slog.Warn("queue worker: delete error", "worker_id", id, "error", err)
		}
	}
}

// runLeased runs one message while keeping its lease alive, and reports whether
// the handler finished.
//
// Both defers are load-bearing and were missing:
//
//   - stopBeat() MUST be deferred. Called straight-line, a panicking handler
//     skipped it: the heartbeat kept ticking (it exits on the pool's ctx, so it
//     is not a permanent leak) and kept EXTENDING the lease of a message the
//     worker had walked away from — so that job was never redelivered for as
//     long as the pool lived. Invisible to goleak, because the goroutine does
//     die at shutdown, and invisible to a "the worker survived" test, because
//     it did. Only "did the job come back?" catches it.
//   - recover() keeps ONE message's failure from being a worker's death. Without
//     it the panic unwound `worker()`, wg.Done ran, and the pool ran with one
//     fewer worker forever; nothing restarts it.
//
// A recovered panic does not consume the job: the caller skips the delete and
// releases the lease, so goqite hands the message to another attempt.
//
// It returns the recovered value (nil when the handler finished), not a bool:
// the caller needs to know WHICH panic happened for its log line, and a bool
// forced a second field or a re-derivation.
func (wp *WorkerPool) runLeased(msg *goqite.Message) (recovered any) {
	// Keep the lease alive for as long as the handler runs, so the visibility
	// timeout can stay short (fast crash recovery) without re-delivering a slow
	// job mid-flight. Nothing called goqite.Extend before; the lease was made 10
	// minutes long instead, which meant a crashed worker's job sat invisible for
	// 10 minutes.
	stopBeat := wp.heartbeat(msg.ID)
	defer stopBeat()

	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("queue worker: handler panicked",
				append(jobAttrs(jobTypeOf(msg.Body)),
					"message_id", msg.ID, "panic", rec, "stack", string(debug.Stack()))...)
			recovered = rec
		}
	}()

	wp.processMessage(context.Background(), msg)
	return nil
}

// heartbeat extends the message's visibility every queueHeartbeatInterval until
// the returned stop func is called. Callers must call it (defer/stop) before
// deleting the message: an extension that lands after the delete is harmless,
// but a goroutine left running is a leak the queue tests would catch.
func (wp *WorkerPool) heartbeat(id goqite.ID) (stop func()) {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		// Snapshotted OUTSIDE the goroutine's loop, before any tick: both are
		// fixed for the life of this heartbeat.
		interval := queueHeartbeatInterval
		if wp.beatInterval > 0 {
			interval = wp.beatInterval // test seam: no 20s wait in the suite
		}
		lease := queueVisibilityTimeout
		if wp.lease > 0 {
			lease = wp.lease // test seam: a short lease so "never redelivered" is observable
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-wp.ctx.Done():
				return
			case <-ticker.C:
				// WithoutCancel: the pool context being cancelled is how we are
				// told to stop, and a cancelled ctx would also fail the extend
				// of a job that is still running fine.
				if err := wp.q.extend(context.WithoutCancel(wp.ctx), id, lease); err != nil {
					if !errors.Is(err, ErrQueueClosed) && !wp.stopping() {
						slog.Warn("queue worker: extend lease failed",
							"message_id", id, "error", err)
					}
					return
				}
			}
		}
	})
	return func() {
		close(done)
		wg.Wait()
	}
}

// releasePanicked makes a message whose handler panicked immediately
// receivable again, instead of waiting out a lease nobody is extending.
//
// This is not belt-and-braces, and both halves were measured against goqite:
//
//   - Stopping the heartbeat does not SHORTEN a lease, it only stops EXTENDING
//     it. The row keeps the `timeout` its last extend wrote, so the message
//     stays invisible for up to a full lease before goqite hands it out again.
//   - While invisible it is gone from the queue's point of view. Leaving it
//     alone is therefore indistinguishable from a crashed process — the right
//     trade for a real crash, the wrong one here: the process is alive and could
//     retry now.
//
// Extending by 0 writes `timeout = now`, the documented way to make a job
// immediately receivable. What makes repeating that safe is the recorded panic:
// a handler that panics every time is retried promptly, and every attempt logs
// its type, message id and stack.
func (wp *WorkerPool) releasePanicked(id goqite.ID) {
	// WithoutCancel: the pool's ctx is already done during shutdown, and
	// releasing a lease is cleanup that must still run then.
	if err := wp.q.extend(context.WithoutCancel(wp.ctx), id, 0); err != nil &&
		!errors.Is(err, ErrQueueClosed) {
		slog.Warn("queue worker: could not release the lease of a panicked job; "+
			"it will be redelivered when the lease expires",
			"message_id", id, "error", err)
	}
}

// processMessage decodes the Job envelope, looks up the handler, and
// invokes it under RetryConfig.Do. The retry layer streams SSE feedback
// ({"type":"retry","attempt":N,"status":"attempt|success"}) between
// attempts so the user sees the retry happening instead of waiting in
// silence.
func (wp *WorkerPool) processMessage(ctx context.Context, msg *goqite.Message) {
	if msg == nil {
		return
	}
	slog.Info("queue worker: processMessage", jobAttrs(jobTypeOf(msg.Body))...)
	job, err := DecodeJob(msg.Body)
	if err != nil {
		// Decode failures are non-retryable: bad bytes will never parse.
		// Log loudly and drop the message rather than spinning forever.
		slog.Error("queue worker: decode failed",
			"worker_id", -1, "error", err, "body", string(msg.Body))
		return
	}

	handler := wp.reg.Lookup(job.Type)
	operation := job.Type
	clientID := job.ClientID

	err = wp.retry.Do(ctx, wp.hub, clientID, operation, func() error {
		return handler(ctx, wp.hub, job)
	})
	if err != nil {
		args := append([]any{"worker_id", -1}, jobAttrs(job.Type)...)
		args = append(args, "client_id", clientID, "error", err)
		slog.Error("queue worker: handler failed after retries", args...)
	}
}

// jobAttrs is the log vocabulary for one job: its type and, when the job is
// recipe-owned (`ext:<recipe>:<job>`), the recipe id. Both the start line and
// the failure line use it, so a job cannot be attributed two ways.
func jobAttrs(jobType string) []any {
	attrs := []any{"type", jobType}
	if id := recipeOf(jobType); id != "" {
		attrs = append(attrs, "recipe", id)
	}
	return attrs
}

// recipeOf extracts the recipe id from a job type. The prefix is the seam's
// own convention (internal/recipe.HasRecipePrefix), so a core job answers "".
func recipeOf(jobType string) string {
	rest, ok := strings.CutPrefix(jobType, "ext:")
	if !ok {
		return ""
	}
	id, _, ok := strings.Cut(rest, ":")
	if !ok || id == "" {
		return ""
	}
	return id
}

func jobTypeOf(body []byte) string {
	var j struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &j); err != nil {
		return ""
	}
	return j.Type
}
