// SCOPE:feature — todo domain events (one fact, every transport)
package todo

import "context"

// Kind is what happened to a todo. Facts, not intents: "updated" carries the
// absolute post-state, so applying it twice converges (a toggle-shaped
// "flip it" event would not).
type Kind string

const (
	Created Kind = "created"
	Updated Kind = "updated"
	Deleted Kind = "deleted"
	// Cleared is the bulk "remove my completed rows" fact.
	Cleared Kind = "cleared"
)

// Event is the one todo fact every consumer sees: the SSE hub (browser),
// the NATS wire (other instances), and any in-process subscriber (a native
// window). Handlers publish it once; adapters decide how it travels.
type Event struct {
	Kind       Kind
	ID         string
	Title      string
	Done       bool
	Owner      string
	Visibility string
	// Origin is the publishing instance (internal/instance). The Bus fills it
	// so a handler cannot forget; an instance ignores its own echo.
	Origin string
	// OriginClient is the originating tab, local-only: it is excluded from the
	// hub fan-out (it already applied the change synchronously) and never
	// travels on the wire.
	OriginClient string
}

// Sink delivers events somewhere (hub, wire, a native channel).
type Sink interface {
	Publish(context.Context, Event)
}
