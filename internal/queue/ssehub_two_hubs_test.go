// SCOPE:core — shared two-instance fixture for the cross-instance bridge tests.

package queue_test

import (
	"testing"

	natsio "github.com/nats-io/nats.go"

	"github.com/calionauta/gogogo/internal/nats"
	"github.com/calionauta/gogogo/internal/queue"
)

// attachFunc is one hub's cross-instance wire. The ephemeral and doc bridges
// have the same shape but disjoint traffic, so the two suites share this
// fixture and differ only in which wire they attach.
type attachFunc func(*queue.SSEHub, *natsio.Conn) error

// twoHubsOn boots an embedded NATS plus two hubs on separate connections (two
// instances of the same app) and attaches wire to each. It returns the second
// connection so a test can close the bus and prove local delivery still works.
func twoHubsOn(t *testing.T, wire attachFunc) (*queue.SSEHub, *queue.SSEHub, *nats.Handle) {
	t.Helper()
	srv, err := nats.StartEmbedded(t.TempDir() + "/nats")
	if err != nil {
		t.Fatalf("embedded nats: %v", err)
	}
	t.Cleanup(srv.Close)

	a, aerr := nats.ConnectExisting(srv.URL())
	if aerr != nil {
		t.Fatalf("connect A: %v", aerr)
	}
	t.Cleanup(a.Close)
	b, berr := nats.ConnectExisting(srv.URL())
	if berr != nil {
		t.Fatalf("connect B: %v", berr)
	}
	t.Cleanup(b.Close)

	hubA, hubB := queue.NewSSEHub(), queue.NewSSEHub()
	t.Cleanup(hubA.Close)
	t.Cleanup(hubB.Close)
	if aerr := wire(hubA, a.Conn); aerr != nil {
		t.Fatalf("attach A: %v", aerr)
	}
	if berr := wire(hubB, b.Conn); berr != nil {
		t.Fatalf("attach B: %v", berr)
	}
	return hubA, hubB, a
}
