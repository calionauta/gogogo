// SCOPE:feature — event sink mapping/scoping tests.
package handlers

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/calionauta/gogogo/features/todo"
	"github.com/calionauta/gogogo/internal/natswire"
	"github.com/calionauta/gogogo/internal/queue"
)

// TestRecordFromEventMapping pins the browser contract: the domain fact maps
// onto the payload the streams already understand ("updated" is "toggled").
func TestRecordFromEventMapping(t *testing.T) {
	cases := []struct {
		ev   todo.Event
		want recordEvent
	}{
		{todo.Event{Kind: todo.Created, ID: "1", Title: "a"}, recordEvent{Event: "created", ID: "1", Title: "a"}},
		{todo.Event{Kind: todo.Updated, ID: "1", Done: true}, recordEvent{Event: "toggled", ID: "1", Done: true}},
		{todo.Event{Kind: todo.Deleted, ID: "1"}, recordEvent{Event: "deleted", ID: "1"}},
		{todo.Event{Kind: todo.Cleared}, recordEvent{Event: "cleared"}},
	}
	for _, c := range cases {
		if got := recordFromEvent(c.ev); got != c.want {
			t.Errorf("recordFromEvent(%+v) = %+v, want %+v", c.ev, got, c.want)
		}
	}
	// The inverse keeps the owner/visibility the wire op was scoped by.
	ev := eventFromRecord(recordEvent{Event: "created", ID: "1", Title: "a"}, "u1", true)
	if ev.Kind != todo.Created || ev.Owner != "u1" || ev.Visibility != todo.VisibilityPublic {
		t.Fatalf("eventFromRecord = %+v", ev)
	}
}

// TestHubSinkScopesAndExcludes pins the two rules the poll era got wrong:
// a private fact reaches only the owner's tabs, a shared one reaches every
// login, and the originating tab never receives its own change.
func TestHubSinkScopesAndExcludes(t *testing.T) {
	hub := queue.NewSSEHub()
	ownerCh := make(chan []byte, 8)
	otherCh := make(chan []byte, 8)
	hub.Register("owner-tab", "u1", ownerCh)
	hub.Register("other-tab", "u2", otherCh)
	defer hub.Unregister("owner-tab")
	defer hub.Unregister("other-tab")

	sink := hubSink{hub: hub}

	// Private: only the owner's tab.
	sink.Publish(context.Background(), todo.Event{
		Kind: todo.Created, ID: "p1", Owner: "u1", Visibility: todo.VisibilityPrivate,
	})
	expectOne(t, ownerCh, "private owner")
	expectNone(t, otherCh, "private other")

	// Public: every login's stream.
	sink.Publish(context.Background(), todo.Event{
		Kind: todo.Created, ID: "p2", Owner: "u1", Visibility: todo.VisibilityPublic,
	})
	expectOne(t, ownerCh, "public owner")
	expectOne(t, otherCh, "public other")

	// Origin excluded: the tab that clicked already patched itself.
	sink.Publish(context.Background(), todo.Event{
		Kind: todo.Created, ID: "p3", Owner: "u1", Visibility: todo.VisibilityPrivate,
		OriginClient: "owner-tab",
	})
	expectNone(t, ownerCh, "origin tab")
}

func expectOne(t *testing.T, ch <-chan []byte, what string) {
	t.Helper()
	select {
	case payload := <-ch:
		var evt recordEvent
		if err := json.Unmarshal(payload, &evt); err != nil || evt.ID == "" {
			t.Fatalf("%s payload = %s", what, payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: no delivery", what)
	}
}

func expectNone(t *testing.T, ch <-chan []byte, what string) {
	t.Helper()
	select {
	case payload := <-ch:
		t.Fatalf("%s: unexpected delivery %s", what, payload)
	case <-time.After(150 * time.Millisecond):
	}
}

// fakeWire records the ops the wire sink maps to.
type fakeWire struct {
	op    natswire.CrudOpType
	owner string
	data  *natswire.CrudOpData
	calls int
}

func (f *fakeWire) Publish(op natswire.CrudOpType, owner string, data *natswire.CrudOpData) {
	f.op, f.owner, f.data, f.calls = op, owner, data, f.calls+1
}

// TestWireSinkMapsFacts pins the fact -> wire translation, including the
// absolute post-state of an update (redelivery-safe on the far side).
func TestWireSinkMapsFacts(t *testing.T) {
	f := &fakeWire{}
	sink := wireSink{pub: f}

	sink.Publish(context.Background(), todo.Event{
		Kind: todo.Created, ID: "1", Title: "a", Owner: "u1", Visibility: todo.VisibilityPublic,
	})
	if f.calls != 1 || f.op != natswire.CrudOpCreate || f.data.Title != "a" || f.data.Visibility != todo.VisibilityPublic {
		t.Fatalf("create -> %+v data=%+v", f.op, f.data)
	}

	sink.Publish(context.Background(), todo.Event{Kind: todo.Updated, ID: "1", Done: true, Owner: "u1"})
	if f.op != natswire.CrudOpToggle || !f.data.Completed {
		t.Fatalf("update -> %+v data=%+v", f.op, f.data)
	}

	sink.Publish(context.Background(), todo.Event{Kind: todo.Deleted, ID: "1", Owner: "u1"})
	if f.op != natswire.CrudOpDelete || f.data.ID != "1" {
		t.Fatalf("delete -> %+v data=%+v", f.op, f.data)
	}

	sink.Publish(context.Background(), todo.Event{Kind: todo.Cleared, Owner: "u1"})
	if f.op != natswire.CrudOpClearCompleted || f.data != nil {
		t.Fatalf("clear -> %+v data=%+v", f.op, f.data)
	}
}
