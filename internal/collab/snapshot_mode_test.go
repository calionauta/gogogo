package collab

import (
	"testing"
)

// TestSnapshotStoreModes pins the COLLAB_SNAPSHOTS switch: memory keeps
// snapshots process-local (draw without persistence), anything unknown
// fails closed (a typo must not silently downgrade durability), and the
// default delegates to the Turso persister (nil queries surface its own
// error, proving the wiring passes queries through). Lives in this package
// (not the server composition test) so trimming the whiteboard bundle
// deletes it with the package it covers.
func TestSnapshotStoreModes(t *testing.T) {
	mem, err := SnapshotStore("memory", nil)
	if err != nil {
		t.Fatalf("memory snapshots = %v, want a store", err)
	}
	if _, ok := mem.(*MemoryPersister); !ok {
		t.Fatalf("memory snapshots = %T, want *MemoryPersister", mem)
	}
	if err := mem.SaveSnapshot("d1", []byte("snap")); err != nil {
		t.Fatalf("memory save = %v", err)
	}
	got, ok := mem.LoadSnapshot("d1")
	if !ok || string(got) != "snap" {
		t.Errorf("memory round-trip = %q,%v, want snap,true", got, ok)
	}
	if _, err := SnapshotStore("WWJD", nil); err == nil {
		t.Error("unknown COLLAB_SNAPSHOTS = nil, want fail-closed error")
	}
	if _, err := SnapshotStore("", nil); err == nil {
		t.Error("default snapshots with nil queries = nil, want the Turso wiring error")
	}
	if _, err := SnapshotStore("TURSO", nil); err == nil {
		t.Error("uppercase TURSO with nil queries = nil, want the Turso " +
			"wiring error (mode is case-insensitive, queries still required)")
	}
}
