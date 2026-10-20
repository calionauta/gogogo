// SCOPE:feature — todo event bus tests (one publish, adapters per transport).
package todo

import (
	"context"
	"sync"
	"testing"
)

// recordSink records every event it receives.
type recordSink struct {
	mu     sync.Mutex
	events []Event
}

func (s *recordSink) Publish(_ context.Context, ev Event) {
	s.mu.Lock()
	s.events = append(s.events, ev)
	s.mu.Unlock()
}

func (s *recordSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

// TestBusPublishesOnceToEveryAdapter is the contract the handlers rely on:
// one Publish call reaches the local sink, the wire sink, and every
// in-process subscriber, all with the instance origin filled in.
func TestBusPublishesOnceToEveryAdapter(t *testing.T) {
	local, wire := &recordSink{}, &recordSink{}
	bus := NewBus(local, "inst-a")
	bus.SetWire(wire)

	var seen []Event
	bus.Subscribe(func(ev Event) { seen = append(seen, ev) })

	bus.Publish(context.Background(), Event{Kind: Created, ID: "t1", Owner: "u1"})

	if local.count() != 1 || wire.count() != 1 {
		t.Fatalf("local=%d wire=%d, want 1/1", local.count(), wire.count())
	}
	if len(seen) != 1 {
		t.Fatalf("subscriber saw %d events, want 1", len(seen))
	}
	if seen[0].Origin != "inst-a" {
		t.Fatalf("origin = %q, want inst-a", seen[0].Origin)
	}
}

// TestBusPublishLocalSkipsWire pins the consumer path: an event that arrived
// from another instance is delivered locally, never re-published.
func TestBusPublishLocalSkipsWire(t *testing.T) {
	local, wire := &recordSink{}, &recordSink{}
	bus := NewBus(local, "inst-a")
	bus.SetWire(wire)

	bus.PublishLocal(context.Background(), Event{Kind: Updated, ID: "t1", Origin: "inst-b"})

	if local.count() != 1 {
		t.Fatalf("local=%d, want 1", local.count())
	}
	if wire.count() != 0 {
		t.Fatalf("wire=%d, want 0 (an echoed event must not go back on the wire)", wire.count())
	}
}

// TestBusWireCanBeDetached: NATS off (or a test) leaves the bus local-only.
func TestBusWireCanBeDetached(t *testing.T) {
	local, wire := &recordSink{}, &recordSink{}
	bus := NewBus(local, "inst-a")
	bus.SetWire(wire)
	bus.SetWire(nil)
	bus.Publish(context.Background(), Event{Kind: Deleted, ID: "t1"})
	if local.count() != 1 || wire.count() != 0 {
		t.Fatalf("local=%d wire=%d, want 1/0", local.count(), wire.count())
	}
}
