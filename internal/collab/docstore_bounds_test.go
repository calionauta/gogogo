// SCOPE:layer=infra,removal=plugin — Loro CRDT + DocStore + sync workers + presence
package collab

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/queue"
)

// stateJSON renders a doc's resolved shapes for state comparisons.
//
// Compare RESOLVED STATE, never EncodeSnapshot() bytes: snapshot bytes are not
// stable for the same logical state, because Loro records every op. Two
// identical `clear` ops produce byte-different snapshots but the same empty
// state, so a byte comparison reports "not idempotent" for a genuinely
// idempotent operation.
func stateJSON(t *testing.T, d *Doc) string {
	t.Helper()
	b, err := json.Marshal(d.Shapes())
	if err != nil {
		t.Fatalf("marshal shapes: %v", err)
	}
	return string(b)
}

// TestDocStoreEvictsAndKeepsState is the regression test for the unbounded
// growth: the store must bound itself, and eviction must not lose data because
// a later access rehydrates from the persister.
func TestDocStoreEvictsAndKeepsState(t *testing.T) {
	p := NewMemoryPersister()
	// idleTTL 0 would mean "use the default"; pass a negative to force
	// immediate eligibility without sleeping.
	store := NewDocStoreWithPersister(p, 4, time.Nanosecond)

	// Create 10 docs, each with a shape, and persist each.
	for i := range 10 {
		id := "doc-" + string(rune('a'+i))
		d := store.GetOrCreate(id)
		if _, err := d.ApplyShapeOp(ShapeOp{Op: "add", Shape: Shape{ID: "s1", Type: "rect", X: 1}}); err != nil {
			t.Fatalf("apply: %v", err)
		}
		if err := p.SaveSnapshot(id, d.EncodeSnapshot()); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	// The cache must be bounded. Some slack is allowed because a doc cannot be
	// evicted while younger than idleTTL, but with a nanosecond TTL every doc
	// is eligible immediately.
	if got := store.Len(); got > 4 {
		t.Errorf("store.Len() = %d, want <= 4 (unbounded growth not fixed)", got)
	}

	// The FIRST doc was almost certainly evicted; re-acquiring it must restore
	// its shape from the persister, not return an empty doc.
	first := store.GetOrCreate("doc-a")
	shapes := first.Shapes()
	if len(shapes) != 1 || shapes[0].ID != "s1" {
		t.Fatalf("rehydrated doc lost its state: got %v, want the persisted shape s1", stateJSON(t, first))
	}
}

// TestDocStoreColdMissDoesNotLoseData is the regression test for the data-loss
// bug that rehydration fixes: an op applied to a doc this process has never
// loaded must merge into the PERSISTED state, not into an empty doc.
//
// This is the reachable production path: the service worker serves the board
// page from cache after a server restart, the SSE stream connects (loading
// nothing), and the client's IndexedDB outbox then replays an op. Before the
// fix that op started from an empty doc and the resolved snapshot was persisted
// over the good one, discarding every shape drawn before.
func TestDocStoreColdMissDoesNotLoseData(t *testing.T) {
	p := NewMemoryPersister()

	// A previous process persisted two shapes.
	seed := NewDoc("doc")
	if _, err := seed.ApplyShapeOp(ShapeOp{Op: "add", Shape: Shape{ID: "old1", Type: "rect", X: 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := seed.ApplyShapeOp(ShapeOp{Op: "add", Shape: Shape{ID: "old2", Type: "rect", X: 2}}); err != nil {
		t.Fatal(err)
	}
	if err := p.SaveSnapshot("doc", seed.EncodeSnapshot()); err != nil {
		t.Fatal(err)
	}

	// A FRESH store (cold process) receives a new op for that doc.
	store := NewDocStoreWithPersister(p, 256, time.Minute)
	worker := NewWebSyncWorker(queue.NewSSEHub(), p, store, nil)

	shapes, err := worker.ApplyOp("doc", "client-1", ShapeOp{Op: "add", Shape: Shape{ID: "new1", Type: "rect", X: 3}})
	if err != nil {
		t.Fatalf("apply op: %v", err)
	}

	ids := map[string]bool{}
	for _, s := range shapes {
		ids[s.ID] = true
	}
	for _, want := range []string{"old1", "old2", "new1"} {
		if !ids[want] {
			t.Errorf("shape %q missing after a cold-miss op — prior shapes were discarded (got %v)", want, shapes)
		}
	}

	// And the persisted snapshot must reflect all three, or a restart loses
	// them again.
	reloaded := NewDoc("doc")
	snap, ok := p.LoadSnapshot("doc")
	if !ok {
		t.Fatal("no snapshot persisted")
	}
	if err := reloaded.ApplyUpdate(snap); err != nil {
		t.Fatal(err)
	}
	if got := len(reloaded.Shapes()); got != 3 {
		t.Errorf("persisted snapshot has %d shapes, want 3: %s", got, stateJSON(t, reloaded))
	}
}

// TestDocStoreNeverEvictsActiveDocs pins the safety property that makes
// eviction correct without reference counting: a doc touched within idleTTL is
// never dropped, even when the cache is over its cap.
func TestDocStoreNeverEvictsActiveDocs(t *testing.T) {
	p := NewMemoryPersister()
	// Large TTL: nothing is eligible.
	store := NewDocStoreWithPersister(p, 2, time.Hour)

	for i := range 8 {
		store.GetOrCreate("active-" + string(rune('a'+i)))
	}
	// Over the cap, but every doc is active, so all 8 must survive. Exceeding
	// the cap is the correct trade: evicting a doc mid-request would put two
	// concurrent ops on two Doc instances and lose one.
	if got := store.Len(); got != 8 {
		t.Errorf("store.Len() = %d, want 8 — an active doc was evicted", got)
	}
}

// TestDocStoreConcurrentAccessIsSafe exercises the lock under -race, including
// eviction racing with access.
func TestDocStoreConcurrentAccessIsSafe(_ *testing.T) {
	p := NewMemoryPersister()
	store := NewDocStoreWithPersister(p, 3, time.Nanosecond)

	var wg sync.WaitGroup
	for i := range 24 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := "doc-" + string(rune('a'+n%8))
			d := store.GetOrCreate(id)
			// Reading forces the doc to be usable while other goroutines may be
			// evicting it.
			_ = d.Shapes()
			store.Len()
		}(i)
	}
	wg.Wait()
}

// TestDocStoreEvictDropsResidentDoc covers the explicit Evict path used to
// reclaim a doc on purpose (e.g. board deletion).
func TestDocStoreEvictDropsResidentDoc(t *testing.T) {
	p := NewMemoryPersister()
	store := NewDocStoreWithPersister(p, 256, time.Minute)

	d := store.GetOrCreate("gone")
	if _, err := d.ApplyShapeOp(ShapeOp{Op: "add", Shape: Shape{ID: "s", Type: "rect"}}); err != nil {
		t.Fatal(err)
	}
	if err := p.SaveSnapshot("gone", d.EncodeSnapshot()); err != nil {
		t.Fatal(err)
	}
	if store.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", store.Len())
	}

	store.Evict("gone")
	if store.Len() != 0 {
		t.Fatalf("Len() = %d after Evict, want 0", store.Len())
	}
	// Re-acquiring rehydrates rather than starting empty.
	back := store.GetOrCreate("gone")
	if len(back.Shapes()) != 1 {
		t.Errorf("re-acquire after Evict lost state: %s", stateJSON(t, back))
	}
}
