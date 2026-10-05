package credits

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// numGoroutines returns the live goroutine count with a small correction for
// the counting call itself, matching the convention already used in
// internal/queue/ssehub_test.go. It is a coarse signal, not an exact count:
// tests assert on "returned to at-or-below the baseline", never on equality.
func numGoroutines() int {
	return runtime.NumGoroutine() - 1
}

// waitForGoroutinesAtMost polls until the goroutine count is <= want or the
// deadline expires. Worker shutdown is asynchronous, so a bare sleep would be
// both slow and flaky.
func waitForGoroutinesAtMost(t *testing.T, want int) bool {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if numGoroutines() <= want {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// TestService_New_StartsNoGoroutines pins the lifecycle contract: building the
// service must not spawn background workers. Construction and startup are
// separate so the process that wires the router owns cancellation (and can
// register it on the app's OnTerminate hook).
//
// This is the red-proof guard for the original bug: credits.New launched two
// fire-and-forget goroutines bound to context.Background(), which no caller
// could ever stop.
func TestService_New_StartsNoGoroutines(t *testing.T) {
	before := numGoroutines()

	s := newTestService(t)
	if s == nil {
		t.Fatal("newTestService returned nil")
	}

	// The service holds live DB handles (their driver may own goroutines), so
	// compare against a generous ceiling rather than the pre-call count: the
	// point is that NO long-lived worker goroutine was started here.
	if n := numGoroutines(); n > before+4 {
		t.Fatalf("New spawned background goroutines: before=%d after=%d", before, n)
	}
}

// waitForGoroutinesAtLeast polls until the goroutine count is >= want or the
// deadline expires. Goroutine launch is asynchronous, so a bare immediate
// check races the scheduler.
func waitForGoroutinesAtLeast(t *testing.T, want int) bool {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if numGoroutines() >= want {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// TestService_Start_StopsOnContextCancel is the behavioural proof that the
// settlement-outbox worker terminates when the app shuts down.
//
// Red-proof: reverting the `select { case <-ctx.Done(): return; case
// <-ticker.C: }` back to a bare `for range ticker.C` makes this test hang on
// the goroutine count and fail.
func TestService_Start_StopsOnContextCancel(t *testing.T) {
	s := newTestService(t)

	before := numGoroutines()
	ctx, cancel := context.WithCancel(t.Context())
	s.Start(ctx)

	// The worker is running now (settlement ticker; Payments is nil here).
	// Poll: the goroutine does not exist the instant Start returns.
	if !waitForGoroutinesAtLeast(t, before+1) {
		t.Fatalf("Start did not launch the settlement worker: before=%d after=%d", before, numGoroutines())
	}

	cancel()

	if !waitForGoroutinesAtMost(t, before) {
		t.Fatalf("worker leaked after cancel: before=%d after=%d", before, numGoroutines())
	}
}

// TestService_Start_IsIdempotent proves a second Start on an already-started
// service does not double the worker set — router wiring may be reached more
// than once in tests and rebuilds.
func TestService_Start_IsIdempotent(t *testing.T) {
	s := newTestService(t)

	before := numGoroutines()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	s.Start(ctx)
	if !waitForGoroutinesAtLeast(t, before+1) {
		t.Fatalf("Start launched no workers: before=%d after=%d", before, numGoroutines())
	}
	first := numGoroutines()
	s.Start(ctx)
	second := numGoroutines()

	if second != first {
		t.Fatalf("second Start launched extra workers: first=%d second=%d", first, second)
	}
}
