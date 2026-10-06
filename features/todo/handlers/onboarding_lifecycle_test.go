// SCOPE:layer=feature,removal=feature — tests for the onboarding handler's
// poll-goroutine lifetime.
package handlers

import (
	"context"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/dagnats"
)

// TestOnboarding_PollRun_StopsOnAppShutdown proves the poll goroutine is bound
// to the handler's shutdown signal, so process shutdown stops it.
//
// Red-proof: with the original `ctx := context.Background()` and no ctx.Done()
// case in the select, shutdown would not stop the loop and this test would
// time out.
func TestOnboarding_PollRun_StopsOnAppShutdown(t *testing.T) {
	// A handler whose client points at a closed port: GetRunRaw fails every
	// tick, exercising the "transient engine error — keep polling" path. That
	// is exactly the path that used to loop forever on a Background context.
	h := &OnboardingHandler{
		client: dagnats.NewClient("http://127.0.0.1:1"),
		done:   make(chan struct{}),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.pollRun("run-does-not-exist")
	}()

	// Let at least one poll tick happen so we know the loop is running.
	select {
	case <-done:
		t.Fatal("pollRun returned before shutdown — it should be polling")
	case <-time.After(2 * onbPollInterval):
	}

	h.shutdown()

	select {
	case <-done:
		// good: shutdown stopped the loop
	case <-time.After(2 * time.Second):
		t.Fatal("pollRun did not stop after shutdown (goroutine leak)")
	}
}

// TestOnboarding_Shutdown_Idempotent pins that a duplicate OnTerminate (or a
// test calling shutdown twice) cannot double-close the channel and panic.
func TestOnboarding_Shutdown_Idempotent(t *testing.T) {
	h := &OnboardingHandler{done: make(chan struct{})}
	h.shutdown()
	h.shutdown() // must not panic

	if !h.shutdownStopped() {
		t.Fatal("done channel not closed after shutdown")
	}
}

// TestOnboarding_ShutdownCtx_CancelOnShutdown proves the context bridge: a
// context obtained from shutdownCtx is cancelled when the handler shuts down.
func TestOnboarding_ShutdownCtx_CancelOnShutdown(t *testing.T) {
	h := &OnboardingHandler{done: make(chan struct{})}
	ctx, cancel := h.shutdownCtx(context.Background())
	defer cancel()

	h.shutdown()

	select {
	case <-ctx.Done():
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("shutdownCtx context not cancelled after shutdown")
	}
}

// TestOnboarding_ShutdownCtx_NilDoneIsSafe pins the defensive path for
// test-constructed handlers that never got a done channel: shutdownCtx must
// still return a usable context, not panic or nil-deref.
func TestOnboarding_ShutdownCtx_NilDoneIsSafe(t *testing.T) {
	h := &OnboardingHandler{} // no done channel
	ctx, cancel := h.shutdownCtx(context.Background())
	defer cancel()
	if ctx == nil {
		t.Fatal("shutdownCtx returned nil context")
	}
	h.shutdown() // must not panic on nil done
}
