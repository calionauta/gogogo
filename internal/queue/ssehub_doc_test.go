// SCOPE:core — SSE hub doc-scoping red-proof.
//
// Collaborative features share ONE hub across docs. A shapes/note-text
// event for docA must reach only streams watching docA: global fan-out
// lets any board/note overwrite every other board/note's view, which is
// how drawings were "erased" and notes duplicated across docs.
package queue

import (
	"testing"
)

// TestSSEHubDocIsolation pins per-doc fan-out: a docA event reaches the
// docA stream and the originator is excluded, while the docB stream
// stays silent.
func TestSSEHubDocIsolation(t *testing.T) {
	h := NewSSEHub()
	chA := make(chan []byte, 8)
	chB := make(chan []byte, 8)
	h.Register("cliA", "", chA)
	h.SetDoc("cliA", "docA")
	h.Register("cliB", "", chB)
	h.SetDoc("cliB", "docB")

	h.BroadcastExceptToDoc([]byte(`{"type":"shapes","doc":"docA"}`), "docA", "cliWriter")

	select {
	case got := <-chA:
		if string(got) != `{"type":"shapes","doc":"docA"}` {
			t.Fatalf("docA stream got %q", got)
		}
	default:
		t.Fatal("docA stream received nothing")
	}
	select {
	case got := <-chB:
		t.Fatalf("cross-doc leak: docB stream received %q", got)
	default:
	}
}

// TestSSEHubDocBroadcastToDoc pins the include-originator variant used
// for authoritative counts: every watcher of the doc gets it, watchers
// of other docs do not.
func TestSSEHubDocBroadcastToDoc(t *testing.T) {
	h := NewSSEHub()
	chA := make(chan []byte, 8)
	chB := make(chan []byte, 8)
	h.Register("cliA", "", chA)
	h.SetDoc("cliA", "docA")
	h.Register("cliB", "", chB)
	h.SetDoc("cliB", "docB")

	h.BroadcastToDoc([]byte(`{"type":"count"}`), "docA")

	select {
	case <-chA:
	default:
		t.Fatal("docA stream received nothing")
	}
	select {
	case got := <-chB:
		t.Fatalf("cross-doc leak: docB stream received %q", got)
	default:
	}
}
