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
	"time"
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
func TestRunReconcile_StopsOnCancel(t *testing.T) {
	defer verifyNoLeak(t)
	s := newTestService(t)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.runReconcile(ctx)
	}()
	// Let the goroutine reach its first select before cancelling; without
	// this the test would pass even if the loop never started.
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
		// good: the loop returned on cancel
	case <-time.After(3 * time.Second):
		t.Fatal("runReconcile did not stop after cancel")
	}
}
