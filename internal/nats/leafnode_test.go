package nats

import (
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

// TestLeafNode_ConnectsToCentral is the regression guard for the edge-sync
// design (Phase B): StartLeafNode must configure the remote correctly so
// the embedded server attaches to the central NATS as a Leaf Node. If the
// remote URL/opts are wrong, the leaf never connects and this fails
// (instead of silently running a standalone server that looks synced but
// isn't).
//
// Note: JetStream stream replication between hub and spoke is a
// domain/account configuration concern handled at deploy time (both sides
// share a JetStream domain); this test asserts the transport binding —
// the leaf actually attaches — which is the part StartLeafNode owns.
func TestLeafNode_ConnectsToCentral(t *testing.T) {
	t.Parallel()
	// Central server with a leaf-node listen port so the spoke can
	// attach. Mirrors what the demo server must expose for desktop edge
	// sync (a NATS_LEAFNODE_URL pointing at this port).
	central := server.New(&server.Options{
		Host:      "127.0.0.1",
		Port:      19433,
		JetStream: true,
		StoreDir:  t.TempDir(),
		NoLog:     true,
		NoSigs:    true,
		LeafNode:  server.LeafNodeOpts{Port: 19434},
	})
	central.Start()
	defer central.Shutdown()
	if !central.ReadyForConnections(10 * time.Second) {
		t.Fatal("central NATS not ready")
	}

	h, err := StartLeafNode(t.TempDir(), "nats://127.0.0.1:19434")
	if err != nil {
		t.Fatalf("StartLeafNode: %v", err)
	}
	t.Cleanup(h.Close)

	// The leaf must have attached to the central. This is the core
	// guard: a misconfigured remote leaves NumLeafNodes at 0 and the
	// edge app would run disjoint from the central.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if central.NumLeafNodes() >= 1 {
			return // leaf attached ✅
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("leaf node did not connect to central (NumLeafNodes=0)")
}
