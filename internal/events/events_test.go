package events

import (
	"context"
	"testing"
	"time"
)

// sinkFunc adapts a function to Sink, so a test records delivery without a
// type per case.
type sinkFunc func(context.Context, Event)

func (f sinkFunc) Deliver(ctx context.Context, event Event) { f(ctx, event) }

func TestBusDeliversToSubscriberAndSink(t *testing.T) {
	bus := New(4)
	bus.Start()
	t.Cleanup(bus.Close)

	delivered := make(chan string, 2)
	unsub := bus.Subscribe(func(_ context.Context, event Event) {
		delivered <- "sub:" + event.Name
	})
	t.Cleanup(unsub)
	bus.AddSink(sinkFunc(func(_ context.Context, event Event) {
		delivered <- "sink:" + event.Name
	}))

	bus.Emit(context.Background(), Event{Name: "recipe.toggled"})

	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case got := <-delivered:
			seen[got] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("delivery timed out, saw %v", seen)
		}
	}
	if !seen["sub:recipe.toggled"] || !seen["sink:recipe.toggled"] {
		t.Fatalf("delivered %v, want the subscriber and the sink", seen)
	}
}

func TestBusCloseStopsDeliveryWithoutPanicking(t *testing.T) {
	bus := New(2)
	bus.Start()
	bus.Close()
	bus.Close() // idempotent: a double shutdown is not a bug

	delivered := make(chan struct{}, 1)
	bus.Subscribe(func(context.Context, Event) { delivered <- struct{}{} })
	bus.Emit(context.Background(), Event{Name: "after.close"})

	select {
	case <-delivered:
		t.Fatal("a closed bus delivered an event")
	case <-time.After(50 * time.Millisecond):
		// The absence is the assertion.
	}
}

func TestBusUnsubscribeStopsTheListener(t *testing.T) {
	bus := New(2)
	bus.Start()
	t.Cleanup(bus.Close)

	calls := make(chan struct{}, 2)
	unsub := bus.Subscribe(func(context.Context, Event) { calls <- struct{}{} })
	unsub()
	bus.Emit(context.Background(), Event{Name: "recipe.toggled"})

	select {
	case <-calls:
		t.Fatal("an unsubscribed listener was called")
	case <-time.After(50 * time.Millisecond):
		// The absence is the assertion.
	}
}
