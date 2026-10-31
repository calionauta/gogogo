// SCOPE:plugin — Federated presence contract tests.
//
// These pin the layer the two feature suites cannot isolate: that List is the
// union (not the local roster), that a peer's retraction removes its ids, that
// an expired lease drops them, that the merged count is re-broadcast, and that
// a doc id never leaks between rosters.
package collab_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/nats"
	"github.com/calionauta/gogogo/internal/queue"
)

// federation boots TWO federated instances (the multi-instance contract is
// always a pair: one instance has no peer to merge) over one embedded NATS, each
// with an empty local PeerSet and a hub whose doc bridge is attached so count
// frames travel.
func federation(t *testing.T) ([]*collab.FederatedPresence, []*queue.SSEHub) {
	t.Helper()
	const n = 2
	srv, err := nats.StartEmbedded(t.TempDir() + "/nats")
	if err != nil {
		t.Fatalf("embedded nats: %v", err)
	}
	t.Cleanup(srv.Close)

	presences := make([]*collab.FederatedPresence, 0, n)
	hubs := make([]*queue.SSEHub, 0, n)
	for i := range n {
		hub := queue.NewSSEHub()
		t.Cleanup(hub.Close)
		conn, cerr := nats.ConnectExisting(srv.URL())
		if cerr != nil {
			t.Fatalf("nats connect %d: %v", i, cerr)
		}
		t.Cleanup(conn.Close)
		if berr := hub.AttachDocBridge(conn.Conn); berr != nil {
			t.Fatalf("doc bridge %d: %v", i, berr)
		}
		// A generous reconnect/dispatch window: the embedded server is shared
		// with whatever else the suite runs in parallel.
		presence := collab.NewFederatedPresence(collab.NewPeerPresence(), hub)
		if aerr := presence.Attach(conn.Conn, fmt.Sprintf("inst-%d", i)); aerr != nil {
			t.Fatalf("federate %d: %v", i, aerr)
		}
		ctx, stop := context.WithCancel(context.Background())
		t.Cleanup(stop)
		go presence.Run(ctx)
		presences = append(presences, presence)
		hubs = append(hubs, hub)
	}
	return presences, hubs
}

// waitUnion polls until every listed instance reports the wanted id set for
// docID (order-insensitive), or fails.
func waitUnion(t *testing.T, docID string, presences []*collab.FederatedPresence, want ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last []string
	for time.Now().Before(deadline) {
		ok := true
		for _, p := range presences {
			got, err := p.List(context.Background(), docID)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			last = got
			if len(got) != len(want) {
				ok = false
				break
			}
			for _, w := range want {
				if !slices.Contains(got, w) {
					ok = false
					break
				}
			}
			if !ok {
				break
			}
		}
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("roster for %q never converged on %v; last saw %v", docID, want, last)
}

// TestFederatedListIsTheUnion is the count bug in one assertion: two instances,
// one connection each, and BOTH must count TWO. With a local roster each said
// one, which is the pill a user actually reads.
//
// RED-PROOFED: drop the remote merge from List and each instance reports 1.
func TestFederatedListIsTheUnion(t *testing.T) {
	p, _ := federation(t)
	if _, err := p[0].Join(context.Background(), "doc-1", "c-a"); err != nil {
		t.Fatalf("join a: %v", err)
	}
	if _, err := p[1].Join(context.Background(), "doc-1", "c-b"); err != nil {
		t.Fatalf("join b: %v", err)
	}
	waitUnion(t, "doc-1", p, "c-a", "c-b")
}

// TestFederatedLeaveRetracts pins the departure: the leaving instance stops
// claiming its connection, and the peer drops it from the union.
//
// RED-PROOFED: stop publishing on Leave and the union keeps c-a until the lease
// expires (>30s), so the poll below times out.
func TestFederatedLeaveRetracts(t *testing.T) {
	p, _ := federation(t)
	ctx := context.Background()
	if _, err := p[0].Join(ctx, "doc-1", "c-a"); err != nil {
		t.Fatalf("join a: %v", err)
	}
	if _, err := p[1].Join(ctx, "doc-1", "c-b"); err != nil {
		t.Fatalf("join b: %v", err)
	}
	waitUnion(t, "doc-1", p, "c-a", "c-b")

	if _, err := p[0].Leave(ctx, "doc-1", "c-a"); err != nil {
		t.Fatalf("leave a: %v", err)
	}
	waitUnion(t, "doc-1", p, "c-b")
}

// TestFederatedDocsStaySeparate pins the doc scope on the ROSTER wire: two
// instances serving two boards must not merge them into one roster.
//
// RED-PROOFED: key the remote map by a constant instead of docID and the doc-2
// roster below lists doc-1's connection.
func TestFederatedDocsStaySeparate(t *testing.T) {
	p, _ := federation(t)
	ctx := context.Background()
	if _, err := p[0].Join(ctx, "doc-1", "c-one"); err != nil {
		t.Fatalf("join doc-1: %v", err)
	}
	if _, err := p[1].Join(ctx, "doc-2", "c-two"); err != nil {
		t.Fatalf("join doc-2: %v", err)
	}
	waitUnion(t, "doc-1", p, "c-one")
	waitUnion(t, "doc-2", p, "c-two")
}

// TestFederatedMergedCountIsRebroadcast pins that a roster change from a peer
// actually reaches the tabs HERE: the merged count must be pushed into the
// local hub, or a tab would only see it on its next reconnect.
//
// RED-PROOFED: remove the broadcastCount call from onSnapshot and the tab's
// channel never receives the peer's id.
func TestFederatedMergedCountIsRebroadcast(t *testing.T) {
	p, hubs := federation(t)
	ctx := context.Background()
	// A tab on instance 0 watching doc-1.
	ch := make(chan []byte, 16)
	hubs[0].Register("tab-local", "u1", ch)
	hubs[0].SetDoc("tab-local", "doc-1")
	defer hubs[0].Unregister("tab-local")

	if _, err := p[0].Join(ctx, "doc-1", "tab-local"); err != nil {
		t.Fatalf("join local: %v", err)
	}
	if _, err := p[1].Join(ctx, "doc-1", "tab-remote"); err != nil {
		t.Fatalf("join remote: %v", err)
	}

	// The tab must eventually receive a count frame listing the remote tab.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case payload := <-ch:
			var msg collab.PresenceMsg
			if err := json.Unmarshal(payload, &msg); err != nil || msg.Type != "count" {
				continue
			}
			if slices.Contains(msg.Peers, "tab-remote") && slices.Contains(msg.Peers, "tab-local") {
				return
			}
		case <-deadline:
			t.Fatal("tab never received a merged count listing the remote peer")
		}
	}
}

// TestFederatedForgetDocDropsBothSides pins the delete path: forgetting a doc
// clears the local roster AND the merged remote entries, so a later List cannot
// resurrect a deleted document's peers.
func TestFederatedForgetDocDropsBothSides(t *testing.T) {
	p, _ := federation(t)
	ctx := context.Background()
	if _, err := p[0].Join(ctx, "doc-1", "c-a"); err != nil {
		t.Fatalf("join a: %v", err)
	}
	if _, err := p[1].Join(ctx, "doc-1", "c-b"); err != nil {
		t.Fatalf("join b: %v", err)
	}
	waitUnion(t, "doc-1", p, "c-a", "c-b")

	if err := p[1].ForgetDoc(ctx, "doc-1"); err != nil {
		t.Fatalf("forget on b: %v", err)
	}
	// Instance B forgot everything it held for the doc…
	got, err := p[1].List(ctx, "doc-1")
	if err != nil {
		t.Fatalf("list b: %v", err)
	}
	for _, id := range got {
		if id == "c-b" {
			t.Fatalf("local roster survived ForgetDoc: %v", got)
		}
	}
	// …and instance A still knows its own connection (forget is per instance).
	found := false
	seen, lerr := p[0].List(ctx, "doc-1")
	if lerr != nil {
		t.Fatalf("list a: %v", lerr)
	}
	for _, id := range seen {
		if id == "c-a" {
			found = true
		}
	}
	if !found {
		t.Fatalf("peer instance lost its own roster to the other's ForgetDoc: %v", seen)
	}
}

// TestFederatedWithoutAttachIsLocal pins the single-instance contract: an
// un-federated instance (NATS off, or GoAkt/trimmed builds) behaves exactly like
// the plain local seam.
func TestFederatedWithoutAttachIsLocal(t *testing.T) {
	hub := queue.NewSSEHub()
	defer hub.Close()
	p := collab.NewFederatedPresence(collab.NewPeerPresence(), hub)

	// Join returns the roster AFTER adding this connection, exactly like the
	// local seam it wraps.
	peers, err := p.Join(context.Background(), "doc-1", "c-a")
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if !slices.Equal(peers, []string{"c-a"}) {
		t.Fatalf("join returned %v; want [c-a]", peers)
	}
	got, lerr := p.List(context.Background(), "doc-1")
	if lerr != nil || !slices.Equal(got, []string{"c-a"}) {
		t.Fatalf("list = %v, %v; want [c-a]", got, lerr)
	}
}
