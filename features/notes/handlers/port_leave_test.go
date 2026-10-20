// SCOPE:feature — Notes presence-leave + caret-ownership contract tests.
//
// Two reports drive this file, both about a cursor moving without its owner
// doing anything:
//
//  1. "I type as demo1 and demo2's cursor keeps drifting inside my box."
//     The heartbeat path force-read ta.selectionStart on an UNFOCUSED
//     textarea, so an idle tab republished the borrowed end-of-value
//     selection as a live caret every 5s. The fix is client-side (see
//     notes.js heartbeatCaret + maybeCaret's focus guard); what the server
//     can prove is that a leave is announced and that a caret report is
//     attributed to its sender, never to a shared identity.
//
//  2. "the tab I closed keeps showing a dot." An EventSource reconnect races
//     its own teardown, and peerLeave's IsRegistered guard then skips the
//     leave. The explicit /leave endpoint (fired from pagehide) must drop
//     the peer set entry and broadcast the departure unconditionally.
package handlers

import (
	"net/http"
	"testing"
)

// TestNotesLeaveEndpointDropsPeerWhenStreamStillRegistered pins the exact
// race the endpoint exists for: the stream is STILL open (so the SSE
// teardown path would have skipped the leave via IsRegistered), yet the
// explicit leave must remove the peer and tell everyone.
//
// RED-PROOFED: point the route at the old IsRegistered-guarded peerLeave
// and the departed peer stays in the count, so the assertion below fails.
func TestNotesLeaveEndpointDropsPeerWhenStreamStillRegistered(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-leave-endpoint"

	logB, closeB := openNoteStream(t, client, server, docID, "cliB")
	settleStream(t, logB, "cliB")
	defer closeB()

	// cliC joins and its stream STAYS open — the reconnect shape.
	_, closeC := openNoteStream(t, client, server, docID, "cliC")
	defer closeC()
	settleStream(t, logB, "cliC")

	waitNoteEvent(t, logB, func(msg map[string]any) bool {
		return msg["type"] == "count" && containsAll(peerIDs(msg), "cliB", "cliC")
	})

	// The tab announces it is going away while its EventSource is alive.
	code := postPresence(t, client, server, docID, "cliC", "leave", "")
	if code != http.StatusOK {
		t.Fatalf("leave endpoint status = %d, want 200", code)
	}

	msg := waitNoteEvent(t, logB, func(msg map[string]any) bool {
		return msg["type"] == "leave" && msg["from"] == "cliC"
	})
	if msg["user"] == "" {
		t.Errorf("leave event lost its display name: %v", msg)
	}
	// The recount must no longer list the departed tab, even though its
	// stream is still registered on the hub.
	count := waitNoteEvent(t, logB, func(msg map[string]any) bool {
		return msg["type"] == "count"
	})
	if containsAll(peerIDs(count), "cliC") {
		t.Fatalf("count after explicit leave still lists cliC: %v", peerIDs(count))
	}
	if !containsAll(peerIDs(count), "cliB") {
		t.Fatalf("count after explicit leave lost the remaining peer: %v", peerIDs(count))
	}
}

// TestNotesLeaveEndpointRequiresSession pins that presence stays signed-in
// only on the new path too: it names who is in a doc.
func TestNotesLeaveEndpointRequiresSession(t *testing.T) {
	server, _, _ := notesPortFixture(t)
	anon := &http.Client{}
	if code := postPresence(t, anon, server, "note-leave-anon", "anonX", "leave", ""); code != http.StatusUnauthorized {
		t.Fatalf("anon leave status = %d, want 401", code)
	}
}

// TestNotesLeaveEndpointNeedsClientID pins the input contract: without a
// clientID there is nothing to remove, and removing "" would be a no-op
// broadcast that lies about a departure.
func TestNotesLeaveEndpointNeedsClientID(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	if code := postPresence(t, client, server, "note-leave-noid", "", "leave", ""); code != http.StatusBadRequest {
		t.Fatalf("leave without clientID status = %d, want 400", code)
	}
}
