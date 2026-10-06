// SCOPE:layer=infra,removal=plugin — Loro CRDT + DocStore + sync workers + presence
package collab

import (
	"encoding/json"
	"errors"
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

// TestStaleOfflineOpIsRejectedNotClobbered is the regression test for the
// lost-update hazard that per-shape versioning fixes.
//
// Scenario: shape s is at x=0. Bob moves it to x=99. Alice, who went offline
// holding x=0, reconnects and replays her queued op setting x=10.
//
// Before versioning her stale write won and Bob's x=99 was silently lost — the
// map value is last-writer-wins and nothing compared revisions. Now the server
// refuses the stale op and reports the conflict, so Bob's edit survives and
// Alice's client can re-apply against the current revision.
func TestStaleOfflineOpIsRejectedNotClobbered(t *testing.T) {
	srv := NewDoc("doc")
	shapes, err := srv.ApplyShapeOp(ShapeOp{Op: "add", Shape: Shape{ID: "s", Type: "rect", X: 0}})
	if err != nil {
		t.Fatal(err)
	}
	aliceSaw := shapes[0].Version // Alice goes offline holding this revision.

	// Bob, online, moves the same shape to x=99 against the revision he saw.
	bobOp := ShapeOp{Op: "add", Shape: Shape{ID: "s", Type: "rect", X: 99}, BaseVersion: aliceSaw}
	if _, bobErr := srv.ApplyShapeOp(bobOp); bobErr != nil {
		t.Fatal(bobErr)
	}

	// Alice reconnects and her stale op replays — refused, not applied.
	current, err := srv.ApplyShapeOp(ShapeOp{
		Op: "add", Shape: Shape{ID: "s", Type: "rect", X: 10}, BaseVersion: aliceSaw,
	})
	if !errors.Is(err, ErrShapeConflict) {
		t.Fatalf("stale offline op err = %v, want ErrShapeConflict", err)
	}

	// Bob's edit survived: this is the bug that was fixed.
	got := srv.Shapes()
	if len(got) != 1 || got[0].X != 99 {
		t.Fatalf("Bob's x=99 was clobbered: %v", got)
	}
	// The conflict response carries the current state so Alice's client can
	// re-apply rather than guess.
	if len(current) != 1 || current[0].X != 99 {
		t.Fatalf("conflict must return current state (x=99), got %v", current)
	}
}

// TestClearDoesNotDeleteConcurrentAdds pins the measurement that REFUTED an
// earlier claim that a replayed `clear` wipes concurrent additions. It does not:
// a clear of key x concurrent with an add of key y converges to {y}.
func TestClearDoesNotDeleteConcurrentAdds(t *testing.T) {
	a := NewDoc("d")
	if _, err := a.ApplyShapeOp(ShapeOp{Op: "add", Shape: Shape{ID: "x", Type: "rect"}}); err != nil {
		t.Fatal(err)
	}
	b := NewDoc("d")
	if err := b.ApplyUpdate(a.EncodeSnapshot()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ApplyShapeOp(ShapeOp{Op: "clear"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ApplyShapeOp(ShapeOp{Op: "add", Shape: Shape{ID: "y", Type: "rect"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.ApplyUpdate(b.EncodeSnapshot()); err != nil {
		t.Fatal(err)
	}
	if err := b.ApplyUpdate(a.EncodeSnapshot()); err != nil {
		t.Fatal(err)
	}
	if sa, sb := stateJSON(t, a), stateJSON(t, b); sa != sb {
		t.Fatalf("peers diverged: a=%s b=%s", sa, sb)
	}
	if got := len(a.Shapes()); got != 1 || a.Shapes()[0].ID != "y" {
		t.Fatalf("want the concurrently-added shape y to survive the clear, got %s", stateJSON(t, a))
	}
}

// TestOpsAreStateIdempotent pins the measurement that REFUTED the claim that a
// blind outbox replay is unsafe: replaying add or clear twice leaves the
// RESOLVED STATE identical.
//
// With versioning in place an exact re-send of an add is now a CONFLICT rather
// than a no-op (the shape already exists at a newer revision), which is the
// correct and safer answer: the duplicate is refused and the client resyncs.
// The state is still unchanged, which is what this test asserts — the property
// that matters for a retrying transport is "no corruption", not "accepted".
func TestOpsAreStateIdempotent(t *testing.T) {
	d := NewDoc("d")
	shapes, err := d.ApplyShapeOp(ShapeOp{Op: "add", Shape: Shape{ID: "s", Type: "rect", X: 5}})
	if err != nil {
		t.Fatal(err)
	}
	afterAdd := stateJSON(t, d)

	// Replay the identical add (same base version 0, as a naive retry would).
	_, replayErr := d.ApplyShapeOp(ShapeOp{Op: "add", Shape: Shape{ID: "s", Type: "rect", X: 5}})
	if !errors.Is(replayErr, ErrShapeConflict) {
		t.Fatalf("replayed add err = %v, want ErrShapeConflict (duplicate refused)", replayErr)
	}
	if got := stateJSON(t, d); got != afterAdd {
		t.Errorf("add replay changed state: %s -> %s", afterAdd, got)
	}

	// An add that correctly carries the current version is the legitimate way
	// to update, and produces a new revision.
	if _, err := d.ApplyShapeOp(ShapeOp{
		Op: "add", Shape: Shape{ID: "s", Type: "rect", X: 6}, BaseVersion: shapes[0].Version,
	}); err != nil {
		t.Fatalf("versioned update: %v", err)
	}

	// clear is still freely idempotent: it takes no version.
	if _, err := d.ApplyShapeOp(ShapeOp{Op: "clear"}); err != nil {
		t.Fatal(err)
	}
	afterClear := stateJSON(t, d)
	if _, err := d.ApplyShapeOp(ShapeOp{Op: "clear"}); err != nil {
		t.Fatal(err)
	}
	if got := stateJSON(t, d); got != afterClear {
		t.Errorf("clear replay changed state: %s -> %s", afterClear, got)
	}
}

// TestShapeVersioningRejectsStaleOp is the regression test for the lost-update
// hazard: an op written against an older revision of a shape must be REFUSED,
// not applied over the newer state.
//
// Scenario: shape s is created (version 1). Bob edits it (version 2). Alice, who
// went offline holding version 1, reconnects and replays her edit based on
// version 1. Before versioning her write won and Bob's was silently lost; now it
// is rejected and the current state is returned so she can re-apply.
func TestShapeVersioningRejectsStaleOp(t *testing.T) {
	d := NewDoc("doc")

	// Draw: a brand-new shape has no stored version, so baseVersion 0 is correct.
	shapes, err := d.ApplyShapeOp(ShapeOp{Op: "add", Shape: Shape{ID: "s", Type: "rect", X: 0}})
	if err != nil {
		t.Fatalf("initial add: %v", err)
	}
	if len(shapes) != 1 || shapes[0].Version != 1 {
		t.Fatalf("after add want 1 shape at version 1, got %v", shapes)
	}
	aliceSaw := shapes[0].Version // 1

	// Bob edits against version 1 — accepted, now version 2.
	shapes, err = d.ApplyShapeOp(ShapeOp{
		Op: "add", Shape: Shape{ID: "s", Type: "rect", X: 99}, BaseVersion: aliceSaw,
	})
	if err != nil {
		t.Fatalf("bob edit: %v", err)
	}
	if shapes[0].Version != 2 || shapes[0].X != 99 {
		t.Fatalf("after bob want version 2 at x=99, got %v", shapes)
	}

	// Alice replays her stale edit, still based on version 1 — must be refused.
	conflict, err := d.ApplyShapeOp(ShapeOp{
		Op: "add", Shape: Shape{ID: "s", Type: "rect", X: 10}, BaseVersion: aliceSaw,
	})
	if !errors.Is(err, ErrShapeConflict) {
		t.Fatalf("stale op err = %v, want ErrShapeConflict", err)
	}
	// The returned state is the CURRENT one, so the client can re-apply.
	if len(conflict) != 1 || conflict[0].X != 99 || conflict[0].Version != 2 {
		t.Fatalf("conflict must return current server state (x=99 v2), got %v", conflict)
	}
	// And the stored state is unchanged — Bob's edit survived.
	got := d.Shapes()
	if len(got) != 1 || got[0].X != 99 {
		t.Fatalf("bob's edit was clobbered: %v", got)
	}

	// A re-apply against the CURRENT version is accepted (the recovery path).
	shapes, err = d.ApplyShapeOp(ShapeOp{
		Op: "add", Shape: Shape{ID: "s", Type: "rect", X: 10}, BaseVersion: 2,
	})
	if err != nil {
		t.Fatalf("re-apply against current version: %v", err)
	}
	if shapes[0].X != 10 || shapes[0].Version != 3 {
		t.Fatalf("re-apply want x=10 v3, got %v", shapes)
	}
}

// TestShapeVersioningRejectsDuplicateIDFromOfflinePeer covers the other half:
// two peers independently draw a shape that ends up with the SAME id (a
// colliding random id, or a client replaying an add it already sent). The second
// add must not silently overwrite the first.
func TestShapeVersioningRejectsDuplicateIDFromOfflinePeer(t *testing.T) {
	d := NewDoc("doc")
	if _, err := d.ApplyShapeOp(ShapeOp{Op: "add", Shape: Shape{ID: "dup", Type: "rect", X: 1}}); err != nil {
		t.Fatal(err)
	}
	// A peer that believes the shape is new (baseVersion 0) must be refused,
	// because the id already exists at version 1.
	dupOp := ShapeOp{Op: "add", Shape: Shape{ID: "dup", Type: "rect", X: 2}}
	if _, dupErr := d.ApplyShapeOp(dupOp); !errors.Is(dupErr, ErrShapeConflict) {
		t.Fatalf("duplicate-id add err = %v, want ErrShapeConflict", dupErr)
	}
	if got := d.Shapes(); len(got) != 1 || got[0].X != 1 {
		t.Fatalf("first shape was overwritten: %v", got)
	}
}

// TestShapeVersionSurvivesSnapshotRoundTrip pins that the version is PERSISTED,
// not just held in memory: after a restart the rehydrated shape must still carry
// its version, or every later edit would be rejected as stale.
func TestShapeVersionSurvivesSnapshotRoundTrip(t *testing.T) {
	src := NewDoc("doc")
	if _, err := src.ApplyShapeOp(ShapeOp{Op: "add", Shape: Shape{ID: "s", Type: "rect", X: 0}}); err != nil {
		t.Fatal(err)
	}
	edit := ShapeOp{Op: "add", Shape: Shape{ID: "s", Type: "rect", X: 5}, BaseVersion: 1}
	if _, err := src.ApplyShapeOp(edit); err != nil {
		t.Fatal(err)
	}

	restored := NewDoc("doc")
	if err := restored.ApplyUpdate(src.EncodeSnapshot()); err != nil {
		t.Fatal(err)
	}
	got := restored.Shapes()
	if len(got) != 1 || got[0].Version != 2 {
		t.Fatalf("version not persisted: got %v, want version 2", got)
	}
	// An edit based on the restored version must be accepted, proving the
	// conflict check works across a restart.
	if _, err := restored.ApplyShapeOp(ShapeOp{
		Op: "add", Shape: Shape{ID: "s", Type: "rect", X: 7}, BaseVersion: 2,
	}); err != nil {
		t.Fatalf("edit after restart rejected: %v", err)
	}
}

// TestClearResetsVersions pins that a clear leaves the doc genuinely empty, so a
// subsequent draw of a fresh shape (baseVersion 0) is accepted rather than being
// treated as a conflict with a tombstoned version.
func TestClearResetsVersions(t *testing.T) {
	d := NewDoc("doc")
	if _, err := d.ApplyShapeOp(ShapeOp{Op: "add", Shape: Shape{ID: "s", Type: "rect"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ApplyShapeOp(ShapeOp{Op: "clear"}); err != nil {
		t.Fatal(err)
	}
	shapes, err := d.ApplyShapeOp(ShapeOp{Op: "add", Shape: Shape{ID: "s", Type: "rect", X: 3}})
	if err != nil {
		t.Fatalf("draw after clear: %v", err)
	}
	if len(shapes) != 1 || shapes[0].Version != 1 {
		t.Fatalf("after clear+draw want 1 shape at version 1, got %v", shapes)
	}
}

// TestStaleOpIsNotPersistedOrBroadcast pins that a rejected op changes nothing:
// no snapshot write, no broadcast. Otherwise a refused write would still reach
// peers (and get persisted) even though the server said no.
func TestStaleOpIsNotPersistedOrBroadcast(t *testing.T) {
	p := NewMemoryPersister()
	store := NewDocStoreWithPersister(p, 64, time.Minute)
	hub := queue.NewSSEHub()
	// A client subscribed so we can detect any broadcast.
	ch := make(chan []byte, 8)
	hub.Register("watcher", "", ch)
	worker := NewWebSyncWorker(hub, p, store, nil)

	if _, err := worker.ApplyOp("doc", "a", ShapeOp{Op: "add", Shape: Shape{ID: "s", Type: "rect", X: 0}}); err != nil {
		t.Fatal(err)
	}
	winOp := ShapeOp{Op: "add", Shape: Shape{ID: "s", Type: "rect", X: 99}, BaseVersion: 1}
	if _, err := worker.ApplyOp("doc", "b", winOp); err != nil {
		t.Fatal(err)
	}
	snapBefore, _ := p.LoadSnapshot("doc")

	// Drain the broadcasts from the two ACCEPTED ops above, so the assertion
	// below can only see a message produced by the rejected op.
	drain := func() int {
		n := 0
		for {
			select {
			case <-ch:
				n++
			default:
				return n
			}
		}
	}
	if n := drain(); n == 0 {
		t.Fatal("expected the accepted ops to have been broadcast (test setup is wrong)")
	}

	// Stale op from a third client still on version 1.
	if _, err := worker.ApplyOp("doc", "c", ShapeOp{
		Op: "add", Shape: Shape{ID: "s", Type: "rect", X: 10}, BaseVersion: 1,
	}); !errors.Is(err, ErrShapeConflict) {
		t.Fatalf("err = %v, want ErrShapeConflict", err)
	}

	snapAfter, _ := p.LoadSnapshot("doc")
	if string(snapBefore) != string(snapAfter) {
		t.Error("a rejected op still rewrote the persisted snapshot")
	}
	select {
	case msg := <-ch:
		t.Errorf("a rejected op was broadcast to peers: %s", msg)
	default:
	}
	// And the state still shows the winning edit.
	got := worker.Shapes("doc")
	if len(got) != 1 || got[0].X != 99 {
		t.Fatalf("state changed after a rejected op: %v", got)
	}
}
