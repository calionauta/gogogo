package credits

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// TestSettlementLoop_VirtualTime drives the PRODUCTION settlement loop with
// testing/synctest, so an hour of ticking completes in milliseconds with no
// real sleep. It pins three contracts at once:
//
//  1. the loop drains on every tick,
//  2. it stops promptly on ctx cancel (no leaked goroutine), and
//  3. the tick cadence is honoured (N ticks for N intervals).
//
// Red-proof: a `for range ticker.C` with no ctx case leaks past the bubble and
// synctest fails the test on a deadlocked/never-exiting goroutine.
func TestSettlementLoop_VirtualTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const tick = time.Minute
		const wantTicks = 5

		var calls atomic.Int64
		ctx, cancel := context.WithCancel(context.Background())
		go settlementLoop(ctx, tick, func(context.Context) {
			calls.Add(1)
		})

		// Advance virtual time: each synctest.Sleep lets the bubble's timers
		// fire without consuming real wall-clock.
		for range wantTicks {
			synctest.Sleep(tick)
			synctest.Wait()
		}

		if got := calls.Load(); got != wantTicks {
			t.Fatalf("drain called %d times after %d ticks, want %d", got, wantTicks, wantTicks)
		}

		// Cancel; synctest.Wait returns only when the loop goroutine has
		// durably blocked or exited — a leaked loop would deadlock the bubble.
		cancel()
		synctest.Wait()

		if got := calls.Load(); got != wantTicks {
			t.Fatalf("loop kept draining after cancel: calls=%d want=%d", got, wantTicks)
		}
	})
}

// TestSettlementLoop_NoTicksNoWork proves the loop does nothing before the
// first interval elapses.
func TestSettlementLoop_NoTicksNoWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int64
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go settlementLoop(ctx, time.Hour, func(context.Context) { calls.Add(1) })

		synctest.Sleep(time.Minute) // well under one hour
		synctest.Wait()

		if got := calls.Load(); got != 0 {
			t.Fatalf("drain ran %d times before the first tick", got)
		}
	})
}
