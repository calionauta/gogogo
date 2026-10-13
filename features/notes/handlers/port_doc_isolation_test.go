// SCOPE:feature — Notes cross-doc isolation red-proof.
//
// Two tabs on DIFFERENT notes share one SSE hub. A note-text event for
// docA must never land on docB's stream: without doc scoping the second
// tab adopts the first tab's text, diffs it against its own base, and
// posts the mix back — ending with two notes each holding part of the
// other's text (the reported duplication bug).
package handlers

import (
	"testing"
	"time"
)

// TestNotesCrossDocIsolation pins that an op on docA is invisible to a
// stream watching docB, while still reaching a stream on docA.
func TestNotesCrossDocIsolation(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docA := "note-isolation-a"
	docB := "note-isolation-b"

	logA, closeA := openNoteStream(t, client, server, docA, "cliA")
	defer closeA()
	logB, closeB := openNoteStream(t, client, server, docB, "cliB")
	defer closeB()
	settleStream(t, logA, "cliA")
	settleStream(t, logB, "cliB")

	postOp(t, client, server, docA, "cliWriter", `{"base":0,"ops":[{"t":"ins","i":0,"s":"secret-a"}]}`)

	// Sanity: the same-doc stream must receive it.
	waitNoteEvent(t, logA, func(msg map[string]any) bool {
		return msg["type"] == "note-text" && msg["doc"] == docA && msg["text"] == "secret-a"
	})

	// The other-doc stream must NOT receive docA's text.
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if msg, ok := logB.find(func(msg map[string]any) bool {
			return msg["type"] == "note-text" && msg["doc"] == docA
		}); ok {
			t.Fatalf("cross-doc leak: docB stream received docA event: %v", msg)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
