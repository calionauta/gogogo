package queue

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/calionauta/gogogo/config"
)

// TestEnqueueDuringClose_NoNilDereference is the regression guard for the
// use-after-close race in Queue.Close.
//
// Close used to do `q.q = nil` and `db.Close()` with no lock at all. Queue had a
// qMu, but every user only ever RLocked it, so it guarded nothing: Enqueue read
// q.q off the struct and drove it with no protection. A goroutine enqueuing
// mid-shutdown therefore dereferenced a nil *goqite.Queue, and one that was
// already inside Send had its *sql.DB closed underneath it.
//
// It is reachable, not theoretical: the shutdown path calls hub.Close() before
// workers exit, which drops every client channel at once, and a handler that
// was mid-Enqueue is running on a request goroutine that http.Server.Shutdown
// does not wait for (SSE streams are hijacked). The comment on workerCount —
// "raise it once contention is measured" — invites the same crash from a
// config change.
//
// RED-proofed: with the old unsynchronized `gq := q.q`, this panics with a nil
// pointer dereference on 3 of 3 runs. With the lock, every Enqueue either lands
// or returns ErrQueueClosed.
func TestEnqueueDuringClose_NoNilDereference(t *testing.T) {
	q, err := New(&config.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const producers, perProducer = 8, 300
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		panicked any
		survived int
	)

	for range producers {
		wg.Go(func() {
			defer func() {
				if r := recover(); r != nil {
					mu.Lock()
					panicked = r
					mu.Unlock()
				}
			}()
			for range perProducer {
				err := q.Enqueue(context.Background(), []byte("payload"))
				// Either a real enqueue or a clean refusal; a nil dereference
				// means Close invalidated the handle while it was in use.
				if err == nil || errors.Is(err, ErrQueueClosed) {
					mu.Lock()
					survived++
					mu.Unlock()
				}
			}
		})
	}

	q.Close()
	wg.Wait()

	if panicked != nil {
		t.Fatalf("Enqueue panicked against a closing queue: %v — Close invalidated the "+
			"goqite handle or the database while a producer was still using it. Every "+
			"access must go through handle() so Close's write lock waits for in-flight "+
			"calls", panicked)
	}
	if survived == 0 {
		t.Fatal("no Enqueue completed, so this proves nothing about the race")
	}
}
