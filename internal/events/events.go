// Domain events: the one seam hooks and analytics subscribe to.
//
// The bus is deliberately small: a closed Event shape, in-process subscribers,
// and outbound sinks. It is non-blocking — a slow webhook never stops a request
// — and it never invents a second audit trail: security_events remains the
// durable security record. Delivery is best-effort in this first cut; a durable
// outbox is the next step, and this package is the seam that makes adding it a
// sink, not a rewrite.
package events

import (
	"context"
	"sync"
	"time"

	"github.com/calionauta/gogogo/internal/observability"
)

// Event is one domain fact. Fields stay a JSON-shaped map so a webhook or an
// analytics provider can read them without importing the producer's types.
type Event struct {
	Name   string         `json:"name"`
	At     time.Time      `json:"at"`
	Fields map[string]any `json:"fields,omitempty"`
}

// Sink receives every event. A sink runs on the bus goroutine, so it must be
// fast or hand off: a sink that blocks delays every event behind it.
type Sink interface {
	Deliver(ctx context.Context, event Event)
}

// Bus fans events out to subscribers and sinks. A full buffer drops the event
// with a warning: backpressure on a request is worse than a lost analytics
// event, and the access log is the authoritative interaction record.
type Bus struct {
	mu      sync.RWMutex
	nextID  int
	subs    map[int]func(context.Context, Event)
	sinks   []Sink
	ch      chan Event
	started bool
	closed  bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// New builds a bus with the given buffer. A zero buffer is promoted to 64.
// It starts nothing: the caller runs Start, so an unused bus costs no goroutine.
func New(buffer int) *Bus {
	if buffer <= 0 {
		buffer = 64
	}
	return &Bus{
		subs: map[int]func(context.Context, Event){},
		ch:   make(chan Event, buffer),
	}
}

// Start runs the delivery goroutine until Close. It is idempotent, and a bus
// that was closed never restarts.
func (b *Bus) Start() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.started || b.closed {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.started = true
	b.cancel = cancel
	b.wg.Go(func() { b.run(ctx) })
}

// Subscribe registers an in-process listener and returns its removal.
func (b *Bus) Subscribe(fn func(context.Context, Event)) func() {
	if b == nil || fn == nil {
		return func() {}
	}
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	b.subs[id] = fn
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		delete(b.subs, id)
		b.mu.Unlock()
	}
}

// AddSink attaches an outbound destination. Sinks run in registration order on
// the bus goroutine. A sink added after Start still sees later events.
func (b *Bus) AddSink(s Sink) {
	if b == nil || s == nil {
		return
	}
	b.mu.Lock()
	b.sinks = append(b.sinks, s)
	b.mu.Unlock()
}

// Emit queues one event. It never blocks the caller, and an event emitted after
// Close is dropped rather than panicking.
func (b *Bus) Emit(ctx context.Context, event Event) {
	if b == nil {
		return
	}
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	// The read lock is held across the send so Close cannot mark the bus
	// closed between the check and the write.
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return
	}
	select {
	case b.ch <- event:
	default:
		observability.Warn(ctx, "events: buffer full, event dropped",
			append(observability.WithComponent("events", "servant"), "event", event.Name)...)
	}
}

// Close stops delivery and waits for the bus goroutine. Events still queued are
// dropped: this bus is a latency-free seam, not a durable log — a durable
// outbox is the documented next step.
func (b *Bus) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	cancel := b.cancel
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	b.wg.Wait()
}

func (b *Bus) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-b.ch:
			b.deliver(ctx, event)
		}
	}
}

func (b *Bus) deliver(ctx context.Context, event Event) {
	b.mu.RLock()
	subs := make([]func(context.Context, Event), 0, len(b.subs))
	for _, fn := range b.subs {
		subs = append(subs, fn)
	}
	sinks := append([]Sink(nil), b.sinks...)
	b.mu.RUnlock()
	observability.Debug(ctx, "events: event",
		append(observability.WithComponent("events", "servant"), "event", event.Name)...)
	for _, fn := range subs {
		fn(ctx, event)
	}
	for _, sink := range sinks {
		sink.Deliver(ctx, event)
	}
}
