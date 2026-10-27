// SCOPE:feature — notifications: in-process inbox change fan-out.
//
// One event per owner whose inbox changed. Subscribers are open SSE streams;
// the bus exists so a delivery written by the DagNats worker reaches tabs that
// are already connected, without the worker knowing anything about HTTP.
//
// Local only, on purpose: a second instance's tabs reconnect to their own
// instance and re-read on open, so the inbox is correct without a cross-instance
// wire. A wire (NATS, like the todo bus) is the change to make when a template
// runs more than one instance with a stream per user — noted in
// docs/notifications.md rather than built before it is needed.
package notifications

import "sync"

// Event announces that one owner's inbox changed. Except is the tab that
// already rendered the change locally (a mark-read POST answers its own
// stream), so it is not woken twice.
type Event struct {
	Owner  string
	Except string
}

// Bus is a tiny fan-out: subscribe returns a cancel func, publish calls every
// subscriber synchronously. Subscribers must not block — a stream handler
// pushes into a size-1 channel and returns.
type Bus struct {
	mu   sync.RWMutex
	next int
	subs map[int]func(Event)
}

// NewBus returns an empty bus.
func NewBus() *Bus { return &Bus{subs: map[int]func(Event){}} }

// Subscribe registers fn and returns the cancel func. Cancel is idempotent.
func (b *Bus) Subscribe(fn func(Event)) func() {
	b.mu.Lock()
	id := b.next
	b.next++
	b.subs[id] = fn
	b.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, id)
			b.mu.Unlock()
		})
	}
}

// Publish fans ev out to the subscribers registered at call time. A
// subscriber removed mid-publish is still called once; its cancel func is
// what stops the stream, so that is harmless.
func (b *Bus) Publish(ev Event) {
	b.mu.RLock()
	subs := make([]func(Event), 0, len(b.subs))
	for _, fn := range b.subs {
		subs = append(subs, fn)
	}
	b.mu.RUnlock()
	for _, fn := range subs {
		fn(ev)
	}
}
