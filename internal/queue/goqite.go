// Package queue implements the background job subsystem: a goqite-backed
// SQLite queue, an SSE hub for streaming progress to the browser, and a
// worker pool that dispatches messages through a typed HandlerRegistry.
//
// Three layered concerns:
//
//   - Queue: durable, persisted in SQLite. Survives restarts.
//   - SSEHub: ephemeral, in-memory fan-out to connected browser tabs.
//   - WorkerPool: drains the queue, dispatches via the registry, retries
//     with exponential backoff, and streams per-attempt feedback.
package queue

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"maragu.dev/goqite"

	"github.com/calionauta/gogogo/config"
)

// dirPerm is the mode for a created queue directory (read/execute for
// group and others so backups and sidecars can inspect it).
const dirPerm = 0o755

// workerCount is the default number of concurrent workers in the pool.
// Four balances throughput against SQLite write contention; raise it
// once contention is measured.
const workerCount = 4

// goqiteSchema is the SQLite DDL for the goqite message table. goqite
// does not auto-migrate, so we apply it on New() if the table does not
// exist yet.
//
//go:embed goqite_schema.sql
var goqiteSchema string

// Queue wraps the underlying goqite queue with an SSE hub for browser
// streaming and a HandlerRegistry for typed dispatch. The struct fields
// are exported via accessor methods rather than directly so callers
// can't bypass Close()'s nil-out of the goqite handle.
type Queue struct {
	// mu guards q and closed, and its rule has two tiers (see handle()):
	// short calls hold it for the whole call so Close waits for them; the long
	// poll releases it before parking so Close is not blocked for a second.
	// Close nils q and closes the database, so a caller that copies the pointer
	// out and uses it later is using a handle whose db is gone — a nil-pointer
	// panic, not an error.
	mu     sync.RWMutex
	q      *goqite.Queue
	closed bool

	hub *SSEHub
	reg *HandlerRegistry
	db  *sql.DB

	// extended is a test seam: called after each successful lease extension so a
	// test can observe the heartbeat without a real 30s wait. Nil in production.
	extended func()

	// workers is the pool StartWorkers created. Close() stops it, so the caller
	// does not have to hold onto the return value to shut the pool down —
	// before this, `StartWorkers()`'s result was routinely discarded and
	// WorkerPool.Stop() was therefore unreachable from production shutdown
	// (workers kept running against a closed database).
	//
	// Both are guarded by mu like q: Close reads and clears them, so an
	// unsynchronized StartWorkers could otherwise hand a pool to a Close that
	// has already run and leave it draining a closed database.
	workers *WorkerPool
	retry   *RetryConfig
}

// New opens a SQLite-backed goqite queue, applies the schema, and returns a
// ready-to-use Queue with a fresh SSEHub and HandlerRegistry. The driver is
// platform-selected (tursogo on desktop/server, ncruces on Android); the
// queue file is just another SQLite file, covered by the same backup.
func New(cfg *config.Config) (*Queue, error) {
	return NewAt(cfg.DataDir)
}

// NewAt opens a queue in an explicit directory (creating it), for stacks
// that need their own queue file — the chi stack runs its own workers on
// a separate file so the two stacks never compete for the same jobs.
func NewAt(dataDir string) (*Queue, error) {
	if err := os.MkdirAll(dataDir, dirPerm); err != nil {
		return nil, fmt.Errorf("queue: create data dir %q: %w", dataDir, err)
	}
	dbPath := dataDir + "/queue.db"
	// busy_timeout must be set in the DSN (a PRAGMA statement is not applied
	// by the driver on open), matching the app DB's DSN in internal/db.
	// Without it a concurrent writer gets SQLITE_BUSY immediately instead of
	// waiting for the lock, so queue writes fail spuriously under contention.
	// Turso manages its own journaling, so no journal_mode pragma is needed.
	db, err := sql.Open(sqlDriver, queueDSN(dbPath))
	if err != nil {
		return nil, fmt.Errorf("queue: open at %s: %w", dbPath, err)
	}

	if err := applyGoqiteSchema(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("queue: apply goqite schema: %w", err)
	}

	gq := goqite.New(goqite.NewOpts{
		DB:      db,
		Name:    "default",
		Timeout: queueVisibilityTimeout,
	})

	return &Queue{
		q:   gq,
		db:  db,
		hub: NewSSEHub(),
		reg: NewHandlerRegistry(),
	}, nil
}

// queueVisibilityTimeout is how long a received job stays invisible before
// goqite re-delivers it (up to MaxReceive=3 total receives).
//
// The 5s library default fits fast jobs, but this queue carries unbounded LLM
// round-trips (chat/suggest): a 30s model call was re-executed twice more while
// still running, and every execution fanned its answer live (persistence
// dedupes by idem key, SSE sends do not — hence triple answers from one click).
// The old fix was to raise the lease to 10 minutes, which covered the slowest
// legitimate job by making a CRASHED worker's job wait 10 minutes for anyone to
// notice — the same constant had to be both "long enough for the slowest job"
// and "short enough to recover fast", and nothing can be both.
//
// A lease is now short and is kept alive: the worker extends it every
// queueHeartbeatInterval for as long as the handler runs (see
// WorkerPool.heartbeat). So a live job is never re-delivered no matter how
// long it takes, and a dead worker is recovered in 30s instead of 10min. No
// goqite.Extend call existed anywhere before, which is what made the long lease
// necessary.
const queueVisibilityTimeout = 30 * time.Second

// queueHeartbeatInterval is how often a leased job has its visibility extended.
// A third of the lease, so two consecutive missed beats still leave the job
// hidden while the worker is alive (a slow scheduler tick is not a crash).
const queueHeartbeatInterval = queueVisibilityTimeout / 3

// applyGoqiteSchema creates the goqite table if it does not exist.
// Safe to call on every startup: the SELECT-then-CREATE pattern is
// idempotent. Uses context.Background() because schema setup must run
// before any caller-driven ctx exists; the operation is local SQLite
// and bounded by the embedded DDL size.
func applyGoqiteSchema(ctx context.Context, db *sql.DB) error {
	var exists bool
	if err := db.QueryRowContext(
		ctx,
		`select exists (select 1 from sqlite_master where type = 'table' and name = 'goqite')`,
	).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := db.ExecContext(ctx, goqiteSchema); err != nil {
		return err
	}
	return nil
}

// ErrQueueClosed is returned when an operation is attempted on a closed queue.
var ErrQueueClosed = errors.New("queue: closed")

// handle returns the goqite queue with the read lock held for as long as the
// caller needs it. ok is false once Close has run: callers then answer
// ErrQueueClosed instead of driving a handle whose database is gone.
//
// TWO-TIER RULE, and it matters which one a caller is on:
//
//   - Short calls (Enqueue, Delete) hold the lock for the whole call. Close
//     blocks until they finish, so the database is never closed under them.
//   - The long poll (ReceiveAndWait) releases the lock BEFORE waiting, because
//     it parks for up to a second and Close would block on it — the pool would
//     deadlock in Stop() waiting for the workers that Close is waiting for.
//     That is safe because database/sql's own contract covers it: DB.Close
//     stops new queries and waits for in-flight ones, so a parked Receive
//     either finishes or fails with "database is closed". The lock exists to
//     stop a nil q dereference, not to serialize the driver.
//
// It returns an unlock func rather than letting callers dereference q directly
// because the failure mode is silent in the wrong shape: `h.queue()` used as a
// plain getter reintroduces exactly the copy-then-use window this lock exists
// to close.
func (q *Queue) handle() (gq *goqite.Queue, unlock func(), ok bool) {
	q.mu.RLock()
	if q.closed || q.q == nil {
		q.mu.RUnlock()
		return nil, func() {}, false
	}
	return q.q, q.mu.RUnlock, true
}

// Enqueue wraps goqite.Send with a typed body. The data must already be
// a marshalled queue.Job envelope when going through HandlerRegistry.
func (q *Queue) Enqueue(ctx context.Context, data []byte) error {
	gq, unlock, ok := q.handle()
	if !ok {
		return ErrQueueClosed
	}
	defer unlock()
	slog.Info("queue: Enqueue called", "body_len", len(data))
	err := gq.Send(ctx, goqite.Message{Body: data})
	slog.Info("queue: Enqueue result", "err", err)
	return err
}

// ReceiveAndWait blocks for up to timeout waiting for a message. It returns
// ErrQueueClosed when the queue is closed, including when Close runs WHILE the
// call is parked: the handle stays valid until this call returns, so Close
// waits for it instead of closing the database underneath it.
func (q *Queue) ReceiveAndWait(ctx context.Context, timeout time.Duration) (*goqite.Message, error) {
	gq, unlock, ok := q.handle()
	if !ok {
		return nil, ErrQueueClosed
	}
	unlock() // see the two-tier rule on handle(): do not hold across the poll
	return gq.ReceiveAndWait(ctx, timeout)
}

// Hub returns the SSEHub for streaming to browser clients.
func (q *Queue) Hub() *SSEHub { return q.hub }

// SetRetry overrides the worker pool's retry backoff. Must be called
// before StartWorkers; a nil/zero config leaves DefaultRetryConfig in
// place. Production never calls this — it exists so a test that has to
// exercise the retry PATH does not also have to sit through the real
// 2s→4s backoff, which makes retry coverage cost seconds per test.
func (q *Queue) SetRetry(cfg RetryConfig) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.retry = &cfg
}

// StartWorkers launches workerCount goroutines that drain the queue
// and dispatch through the HandlerRegistry. Returns the pool so
// callers can stop it explicitly on shutdown.
func (q *Queue) StartWorkers() *WorkerPool {
	q.mu.Lock()
	if q.closed {
		// Nothing to drain: the queue is gone, and a pool started here would
		// spend its life logging ErrQueueClosed. Return an inert pool so the
		// caller's Stop() is still a no-op rather than a nil dereference.
		q.mu.Unlock()
		wp := NewWorkerPool(q, q.hub, q.reg, workerCount)
		return wp
	}
	wp := NewWorkerPool(q, q.hub, q.reg, workerCount)
	if q.retry != nil {
		wp.SetRetry(*q.retry)
	}
	q.workers = wp
	q.mu.Unlock()

	wp.Start()
	return wp
}

// receiveAndWait and delete are the worker pool's two queue calls. The pool
// used to hold the raw *goqite.Queue, which meant Close could nil the field and
// close the database while a worker was between qGuard() and Receive — a
// nil-pointer send in the pool, and a dead driver underneath it.
func (q *Queue) receiveAndWait(ctx context.Context, timeout time.Duration) (*goqite.Message, error) {
	return q.ReceiveAndWait(ctx, timeout)
}

// extend keeps a leased message invisible for another full lease. It returns
// ErrQueueClosed during shutdown so a heartbeat stops cleanly instead of
// logging a failure it cannot act on.
func (q *Queue) extend(ctx context.Context, id goqite.ID, lease time.Duration) error {
	gq, unlock, ok := q.handle()
	if !ok {
		return ErrQueueClosed
	}
	defer unlock()
	if err := gq.Extend(ctx, id, lease); err != nil {
		return err
	}
	if q.extended != nil {
		q.extended()
	}
	return nil
}

func (q *Queue) deleteMessage(ctx context.Context, id goqite.ID) error {
	gq, unlock, ok := q.handle()
	if !ok {
		return ErrQueueClosed
	}
	defer unlock()
	return gq.Delete(ctx, id)
}

// Registry returns the handler registry. Production code calls
// q.Registry().Register("type", handler) before StartWorkers so the
// worker pool can dispatch incoming messages. Tests use the same API
// to inject fake handlers.
func (q *Queue) Registry() *HandlerRegistry { return q.reg }

// Close drains in-flight workers via the SSE hub and shuts down the
// underlying database. After Close, Receive/Delete return ErrQueueClosed.
// Close stops the worker pool (when StartWorkers was called), then releases the
// queue handle and the database.
//
// Stopping the pool FIRST matters: the workers loop on the queue and the DB, so
// closing the database out from under them leaves them logging receive errors
// against a nil/dead handle instead of exiting. WorkerPool.Stop waits for every
// worker goroutine, which is also what makes this a bounded shutdown rather than
// a race with the process exiting.
func (q *Queue) Close() {
	// Stop the pool BEFORE taking the write lock: Stop waits for the workers,
	// and a worker is inside a queue call holding the read lock at this very
	// moment. Taking the write lock first would deadlock against them.
	q.mu.Lock()
	wp := q.workers
	q.workers = nil
	q.mu.Unlock()
	if wp != nil {
		wp.Stop()
	}
	// Drop live SSE registrations so shutdown holds no stale channels.
	// Live streams still exit via their request context (HTTP shutdown);
	// Close only clears hub state and never blocks producers.
	if q.hub != nil {
		q.hub.Close()
	}
	// Only now invalidate the handle. The write lock waits for any in-flight
	// Enqueue/Receive/Delete (and any pool that was built by another goroutine
	// and has not started yet) to return, so the database is closed after its
	// last user, never under it.
	q.mu.Lock()
	q.closed = true
	q.q = nil
	db := q.db
	q.db = nil
	q.mu.Unlock()
	// In-flight Receives are already past handle() and parked in the driver;
	// DB.Close stops new queries and waits for those to finish.
	if db != nil {
		_ = db.Close()
	}
}
