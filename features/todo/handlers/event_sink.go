// SCOPE:feature — todo event sinks (hub + NATS adapters)
package handlers

import (
	"context"
	"encoding/json"

	"github.com/calionauta/gogogo/features/todo"
	"github.com/calionauta/gogogo/internal/natswire"
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/queue"
)

// hubSink turns a domain Event into the record-event JSON the streams
// already understand, scoped by visibility. The browser contract does not
// change: private rows reach the owner's tabs, shared rows reach every
// login's streams, and the originating tab is excluded (it patched itself).
type hubSink struct {
	hub *queue.SSEHub
}

func (s hubSink) Publish(ctx context.Context, ev todo.Event) {
	payload, err := json.Marshal(recordFromEvent(ev))
	if err != nil {
		obsArgs := append(observability.WithComponent("todo", "feature"), "error", err)
		observability.Error(ctx, "todo: marshal record event", obsArgs...)
		return
	}
	if ev.Visibility == todo.VisibilityPublic {
		s.hub.BroadcastToAllUsers(payload, ev.OriginClient)
		return
	}
	s.hub.BroadcastToUser(payload, ev.Owner, ev.OriginClient)
}

// recordFromEvent maps the domain fact onto the stream's payload. The
// "toggled" name is the existing browser contract; the domain calls it
// Updated because an edit uses the same fact shape.
func recordFromEvent(ev todo.Event) recordEvent {
	switch ev.Kind {
	case todo.Created:
		return recordEvent{Event: recordCreated, ID: ev.ID, Title: ev.Title}
	case todo.Updated:
		return recordEvent{Event: recordToggled, ID: ev.ID, Done: ev.Done}
	case todo.Deleted:
		return recordEvent{Event: recordDeleted, ID: ev.ID}
	case todo.Cleared:
		return recordEvent{Event: recordCleared}
	default:
		return recordEvent{}
	}
}

// eventFromRecord is the inverse, for the consumer: it applied a wire op and
// now tells local subscribers about the resulting fact.
func eventFromRecord(evt recordEvent, owner string, shared bool) todo.Event {
	ev := todo.Event{Owner: owner, Visibility: todo.VisibilityPrivate}
	if shared {
		ev.Visibility = todo.VisibilityPublic
	}
	switch evt.Event {
	case recordCreated:
		ev.Kind, ev.ID, ev.Title = todo.Created, evt.ID, evt.Title
	case recordToggled:
		ev.Kind, ev.ID, ev.Done = todo.Updated, evt.ID, evt.Done
	case recordDeleted:
		ev.Kind, ev.ID = todo.Deleted, evt.ID
	case recordCleared:
		ev.Kind = todo.Cleared
	}
	return ev
}

// wirePublisher is the one method the wire sink needs, so the mapping is
// testable without a NATS server (and the sink depends on the shape, not on
// the concrete publisher).
type wirePublisher interface {
	Publish(natswire.CrudOpType, string, *natswire.CrudOpData)
}

// wireSink maps the fact onto the cross-instance wire. The publisher stamps
// Origin (this instance) so the publishing instance ignores its own echo.
type wireSink struct {
	pub wirePublisher
}

func (s wireSink) Publish(_ context.Context, ev todo.Event) {
	switch ev.Kind {
	case todo.Created:
		s.pub.Publish(natswire.CrudOpCreate, ev.Owner,
			&natswire.CrudOpData{ID: ev.ID, Title: ev.Title, Visibility: ev.Visibility})
	case todo.Updated:
		s.pub.Publish(natswire.CrudOpToggle, ev.Owner,
			&natswire.CrudOpData{ID: ev.ID, Completed: ev.Done})
	case todo.Deleted:
		s.pub.Publish(natswire.CrudOpDelete, ev.Owner, &natswire.CrudOpData{ID: ev.ID})
	case todo.Cleared:
		s.pub.Publish(natswire.CrudOpClearCompleted, ev.Owner, nil)
	}
}
