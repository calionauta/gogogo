// SCOPE:feature — todo event bus (one publish, adapters per transport)
package todo

import (
	"context"
	"sync"
)

// Bus is the single publish point for todo facts. Handlers call Publish
// once; the local sink (SSE hub) and the optional wire sink (JetStream)
// are adapters, and in-process subscribers (a native frontend) get the
// typed Event without any transport in between.
//
// Convention over configuration: a single-process deployment wires only the
// local sink, and nothing changes. The wire sink is attached when JetStream
// is; a nil wire means "this instance is the only one".
type Bus struct {
	mu    sync.RWMutex
	local Sink
	wire  Sink
	// origin tags published events with this process's identity.
	origin string
	subs   []func(Event)
}

// NewBus builds the bus around the local (hub) sink. origin comes from
// internal/instance; an empty one leaves events untagged, which readers treat
// as remote (never drop).
func NewBus(local Sink, origin string) *Bus {
	return &Bus{local: local, origin: origin}
}

// SetWire attaches the cross-instance sink (nil detaches it). Safe to call
// after the bus is serving: AttachJetStream runs after the handler exists.
func (b *Bus) SetWire(w Sink) {
	b.mu.Lock()
	b.wire = w
	b.mu.Unlock()
}

// Subscribe registers an in-process listener (a native frontend, a test).
// Listeners are called synchronously on the publishing goroutine and must
// not block: a slow subscriber would stall the mutation's fan-out.
func (b *Bus) Subscribe(fn func(Event)) {
	if fn == nil {
		return
	}
	b.mu.Lock()
	b.subs = append(b.subs, fn)
	b.mu.Unlock()
}

// Publish fans one event out to every adapter and subscriber.
func (b *Bus) Publish(ctx context.Context, ev Event) {
	b.PublishLocal(ctx, ev)
	b.mu.RLock()
	wire := b.wire
	b.mu.RUnlock()
	if wire != nil {
		wire.Publish(ctx, ev)
	}
}

// PublishLocal delivers to the local hub and in-process subscribers only.
// The remote consumer uses it after applying an op: re-publishing to the
// wire would echo the event back to every instance.
func (b *Bus) PublishLocal(ctx context.Context, ev Event) {
	if ev.Origin == "" {
		ev.Origin = b.origin
	}
	b.mu.RLock()
	local := b.local
	subs := append([]func(Event){}, b.subs...)
	b.mu.RUnlock()
	if local != nil {
		local.Publish(ctx, ev)
	}
	for _, fn := range subs {
		fn(ev)
	}
}
