// SCOPE:layer=infra,removal=core — Queue handlers + retry + workers
package queue

import (
	"context"
	"encoding/json"
	"log/slog"
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
	// loop. It is a wait, not a sleep — see worker() for why that distinction is
	// load-bearing.
	receiveErrorBackoff = time.Second
)

// waitCtx waits for d, or returns early when ctx is cancelled.
//
// Production code must not call time.Sleep: an uninterruptible wait holds its
// goroutine — and therefore any wg.Wait() draining it at shutdown — for its
// full duration, ignoring cancellation entirely. WorkerPool.Stop() is exactly
// such a drain, so a bare sleep in a worker delays shutdown and defeats the
// cancel path that exists to make shutdown prompt.
func waitCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

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
	q        *goqite.Queue
	qMu      sync.RWMutex // protects q against concurrent Close()
	hub      *SSEHub
	reg      *HandlerRegistry
	retry    *RetryConfig
	count    int
	stopCh   chan struct{}
	wg       sync.WaitGroup
	ctx      context.Context //nolint:containedctx // lifecycle context for the pool, not request-scoped
	cancel   context.CancelFunc
	stopOnce sync.Once
}

// NewWorkerPool wires a worker pool with default retry settings
// (DefaultRetryConfig: 3 attempts, 2s→30s exponential backoff, ±20% jitter).
// Call SetRetry to override.
func NewWorkerPool(q *goqite.Queue, hub *SSEHub, reg *HandlerRegistry, count int) *WorkerPool {
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

		q := wp.qGuard()
		if q == nil {
			return // Queue was closed; stop draining.
		}

		msg, err := q.ReceiveAndWait(wp.ctx, time.Second)
		if err != nil {
			if wp.stopping() {
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

		wp.processMessage(context.Background(), msg)

		if q := wp.qGuard(); q != nil {
			if err := q.Delete(context.Background(), msg.ID); err != nil {
				slog.Warn("queue worker: delete error", "worker_id", id, "error", err)
			}
		}
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
	slog.Info("queue worker: processMessage", "type", jobTypeOf(msg.Body))
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
		slog.Error("queue worker: handler failed after retries",
			"worker_id", -1, "type", job.Type, "client_id", clientID, "error", err)
	}
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

// qGuard returns the underlying goqite queue or nil if it has been closed.
func (wp *WorkerPool) qGuard() *goqite.Queue {
	wp.qMu.RLock()
	defer wp.qMu.RUnlock()
	return wp.q
}
