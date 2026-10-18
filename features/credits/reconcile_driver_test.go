// SCOPE:plugin — reconcile driver contract tests.
//
// Reconcile existed in the library but was never called in production, so
// drift between reservations and ledger went undetected. These tests pin the
// driver: a clean ledger reconciles with no mismatches, and the background
// loop stops on cancel without leaking.
package credits

import (
	"context"
	"testing"
	"testing/synctest"
)

// TestReconcile_CleanLedgerHasNoDrift proves the driver exercises the real
// path: on a fresh ledger (one monthly grant, no reservations) Reconcile
// reports nothing. If the call broke — wrong ctx, wrong DB — this fails
// instead of the ticker silently logging errors forever.
func TestReconcile_CleanLedgerHasNoDrift(t *testing.T) {
	s := newTestService(t)
	mismatches, err := s.Credits.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile on a clean ledger: %v", err)
	}
	if len(mismatches) != 0 {
		t.Fatalf("clean ledger reported %d mismatches: %+v", len(mismatches), mismatches)
	}
}

// TestRunReconcile_StopsOnCancel proves the background loop honours its
// lifetime: start it, cancel, and it must return — the same contract the
// settlement loop test pins. The 10-minute production interval never fires
// here; cancel fires first, which is exactly the shutdown path.
//
// DETERMINISTIC, no real clock. It used to `time.Sleep(50ms)` "to let the
// goroutine reach its first select", with a 3s `time.After` backstop on the
// assertion — a 50ms guess that the scheduler would get there, and a 3s budget
// that a loaded box could blow. `synctest.Wait()` is the documented replacement
// and it is not a longer sleep: it returns only when every goroutine in the
// bubble is DURABLY BLOCKED, so it proves the loop reached its select rather
// than hoping it did. No wall-clock is consumed either way.
//
// The bubble is why this works: `runReconcile` blocks on a ticker and ctx,
// both of which synctest virtualizes. The service's sqlite handle is created
// OUTSIDE the bubble by newTestService — that is deliberate, because external
// I/O is NOT durably blocking and would defeat the whole point.
func TestRunReconcile_StopsOnCancel(t *testing.T) {
	// Built before the bubble: the DB is real I/O, and synctest requires
	// external dependencies to sit outside it.
	s := newTestService(t)
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			s.runReconcile(ctx)
		}()

		// Not a sleep: this returns once the loop goroutine is parked on its
		// select, which is the fact the old 50ms was guessing at.
		synctest.Wait()
		cancel()
		synctest.Wait()

		select {
		case <-done:
			// good: the loop returned on cancel
		default:
			t.Fatal("runReconcile did not stop after cancel (still running after the bubble settled)")
		}
	})
}
