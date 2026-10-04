package collab

import (
	"sync"
	"testing"
	"time"

	natsio "github.com/nats-io/nats.go"

	"github.com/calionauta/gogogo/internal/nats"
)

// fakePersister records snapshots by docID so the test can assert the
// SyncWorker converged + persisted without a real PocketBase.
type fakePersister struct {
	mu        sync.Mutex
	snapshots map[string][]byte
}

func (f *fakePersister) SaveSnapshot(docID string, snapshot []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshots[docID] = snapshot
	return nil
}

func (f *fakePersister) LoadSnapshot(docID string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.snapshots[docID]
	return s, ok
}

func (f *fakePersister) get(docID string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshots[docID]
}

// TestCollab_SyncWorkerPersists is the Phase C regression guard: a Loro
// update published on app.sync.<docID> must be applied by the SyncWorker
// and persisted via the Persister. This proves the whole edge→central
// sync path (transport + CRDT merge + persist) works end-to-end with a
// real NATS JetStream — not just that the types compile.
func TestCollab_SyncWorkerPersists(t *testing.T) {
	// Embedded NATS with JetStream (standalone; in production the desktop
	// is a Leaf Node and the worker runs on the central — same code path).
	if err := nats.StartEmbedded(t.TempDir()); err != nil {
		t.Fatalf("nats start: %v", err)
	}
	defer nats.Stop()

	nc, err := natsio.Connect(nats.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	defer nc.Close()

	fp := &fakePersister{snapshots: make(map[string][]byte)}
	docs := NewDocStore()
	worker := NewSyncWorker(nc, fp, docs)
	workerCtx := t.Context()
	go func() { _ = worker.Run(workerCtx) }()

	// Simulate the desktop edge: create a Loro doc, make a change,
	// publish the update on the sync subject. Publish in a retry loop so a
	// slow worker subscription (JetStream init on CI) does not drop the
	// only message — core NATS does not replay lost core messages.
	edgeDoc := NewDoc("wb-123")
	// A real edit would go through edgeDoc.Text(...).Insert(...); here we
	// just export the (empty→initial) update to exercise the pipeline.
	update, err := edgeDoc.EncodeUpdate(nil)
	if err != nil {
		t.Fatalf("encode update: %v", err)
	}
	publishDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(publishDeadline) {
		if len(fp.get("wb-123")) > 0 {
			break
		}
		if err := nc.Publish("app.sync.wb-123", update); err != nil {
			t.Fatalf("publish: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// The worker must apply + persist. Allow time for JetStream deliver.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if len(fp.get("wb-123")) > 0 {
			// Snapshot persisted — now verify it decodes as a valid Loro
			// snapshot (i.e. the worker's ApplyUpdate + EncodeSnapshot
			// round-tripped through the CRDT).
			centralDoc := NewDoc("wb-123")
			if err := centralDoc.ApplyUpdate(fp.get("wb-123")); err != nil {
				t.Fatalf("persisted snapshot not a valid Loro doc: %v", err)
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("SyncWorker did not persist the synced doc within timeout")
}
