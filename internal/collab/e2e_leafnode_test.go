package collab

import (
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	natsio "github.com/nats-io/nats.go"
)

// leafCentralPort is the port the central server listens on for leaf-node
// connections. Fixed (not ephemeral) because the leaf remote must name a
// concrete address to dial; the HTTP side stays ephemeral via Port: -1.
const leafCentralPort = 17422

// TestCollab_LeafNodeE2E is the edge-sync e2e guard: a real central NATS
// (embedded, JetStream, with a Leaf Node listen port) runs the SyncWorker;
// a SEPARATE leaf-node server (the desktop edge) connects to it and
// publishes a Loro update on app.sync.<docID>. The leaf node replicates the
// message to central; the worker applies it and persists. This exercises
// the exact production path (Phase B transport + Phase C merge/persist)
// without mocking the leaf attachment.
//
// The body reads as the five steps of that story; each step is a helper so a
// failure names the stage that broke instead of pointing at one long function.
func TestCollab_LeafNodeE2E(t *testing.T) {
	central, centralNC := startLeafCentral(t)

	fp, docs := &fakePersister{snapshots: make(map[string][]byte)}, NewDocStore()
	worker := NewSyncWorker(centralNC, fp, docs)
	go func() { _ = worker.Run(t.Context()) }()

	_, leafNC := startLeafEdge(t)

	waitForLeafAttached(t, central)

	update := encodeEdgeUpdate(t)
	publish := func() { publishLeafUpdate(t, leafNC, update) }
	publish()

	waitForPersisted(t, fp, publish)
}

// startLeafCentral boots the central server (JetStream + a leaf-node listen
// port) and returns it with a client connection. Both are torn down on cleanup.
func startLeafCentral(t *testing.T) (*server.Server, *natsio.Conn) {
	t.Helper()

	central, err := server.NewServer(&server.Options{
		Port:      -1,
		NoLog:     true,
		NoSigs:    true,
		StoreDir:  t.TempDir(),
		JetStream: true,
		LeafNode: server.LeafNodeOpts{
			Host: "127.0.0.1",
			Port: leafCentralPort,
		},
	})
	if err != nil {
		t.Fatalf("central server: %v", err)
	}
	central.Start()
	t.Cleanup(central.Shutdown)
	if !central.ReadyForConnections(10 * time.Second) {
		t.Fatal("central never ready")
	}

	nc, err := natsio.Connect(central.ClientURL())
	if err != nil {
		t.Fatalf("central connect: %v", err)
	}
	t.Cleanup(nc.Close)
	return central, nc
}

// startLeafEdge boots the edge (desktop) leaf-node server and returns it with a
// client connection. The edge is what produces the update under test.
func startLeafEdge(t *testing.T) (*server.Server, *natsio.Conn) {
	t.Helper()

	centralURL := &url.URL{Scheme: "nats", Host: fmt.Sprintf("127.0.0.1:%d", leafCentralPort)}
	leaf, err := server.NewServer(&server.Options{
		Port:      -1,
		NoLog:     true,
		NoSigs:    true,
		StoreDir:  t.TempDir(),
		JetStream: true,
		LeafNode: server.LeafNodeOpts{
			Remotes: []*server.RemoteLeafOpts{{URLs: []*url.URL{centralURL}}},
		},
	})
	if err != nil {
		t.Fatalf("leaf server: %v", err)
	}
	leaf.Start()
	t.Cleanup(leaf.Shutdown)
	if !leaf.ReadyForConnections(10 * time.Second) {
		t.Fatal("leaf never ready")
	}

	nc, err := natsio.Connect(leaf.ClientURL())
	if err != nil {
		t.Fatalf("leaf connect: %v", err)
	}
	t.Cleanup(nc.Close)
	return leaf, nc
}

// waitForLeafAttached blocks until central sees the edge's leaf connection.
//
// Attachment is necessary but NOT sufficient for delivery: the edge may publish
// before the worker's app.sync.> interest has propagated to the leaf, in which
// case the leaf has nothing to forward the message to and the update is
// silently dropped. waitForPersisted compensates by re-publishing rather than
// relying on a propagation signal — see its comment for why counting central's
// subscriptions is not one.
func waitForLeafAttached(t *testing.T, central *server.Server) {
	t.Helper()

	for range 50 {
		if central.NumLeafNodes() > 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("leaf node never attached to central")
}

// encodeEdgeUpdate produces the CRDT delta the edge publishes.
func encodeEdgeUpdate(t *testing.T) []byte {
	t.Helper()

	update, err := NewDoc("e2e-doc").EncodeUpdate(nil)
	if err != nil {
		t.Fatalf("encode update: %v", err)
	}
	return update
}

// publishLeafUpdate sends the update over the edge's connection.
func publishLeafUpdate(t *testing.T, leafNC *natsio.Conn, update []byte) {
	t.Helper()

	if err := leafNC.Publish("app.sync.e2e-doc", update); err != nil {
		t.Fatalf("leaf publish: %v", err)
	}
}

// waitForPersisted polls until the central worker has persisted a snapshot,
// re-publishing each round.
//
// Re-publishing (rather than sending once and waiting) is what makes this
// deterministic: the first attempt can still be dropped if the worker's
// interest has not reached the leaf yet, and a single send plus a 15s wait
// cannot recover from that. A Loro update is an idempotent CRDT delta, so
// repeated delivery converges to the same snapshot — which makes this strictly
// stronger than waiting for a readiness signal we do not have: central's
// NumSubscriptions() is already 63+ from the server's own JetStream and leaf
// plumbing, so any `> 0` test on it is trivially true (measured).
//
// The failure it prevents was observed as "leaf-node update was not replicated
// to central + persisted" after the full deadline, but only under load (16s vs
// the usual 2.3s for this package) — which is what made it read as an
// infrastructure flake rather than a missing wait.
func waitForPersisted(t *testing.T, fp *fakePersister, publish func()) {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if snapshot := fp.get("e2e-doc"); len(snapshot) > 0 {
			if err := NewDoc("e2e-doc").ApplyUpdate(snapshot); err != nil {
				t.Fatalf("persisted snapshot invalid Loro doc: %v", err)
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
		publish()
	}
	t.Fatal("leaf-node update was not replicated to central + persisted")
}
