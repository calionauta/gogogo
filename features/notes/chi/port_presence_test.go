// SCOPE:feature — Notes presence + caret chi port tests

package chi

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

// TestNotesStreamSendsSessionEvent pins session visibility: a tab open
// for days outlives its cookie, and without this event an expired
// session looks identical to a healthy one.
func TestNotesStreamSendsSessionEvent(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-session"

	got, closeStream := openNoteStream(t, client, server, docID, "cliB")
	defer closeStream()

	waitNoteEvent(t, got, func(msg map[string]any) bool {
		authed, _ := msg["authed"].(bool)
		return msg["type"] == "session" && authed
	})
}

// TestNotesOpReportsAuthed pins both branches: authed ops report true,
// anonymous ops still apply (warn-only) but report false.
func TestNotesOpReportsAuthed(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-authedflag"

	_, out := postOp(t, client, server, docID, "cliA", `{"base":0,"ops":[{"t":"ins","i":0,"s":"hi"}]}`)
	if authed, _ := out["authed"].(bool); !authed {
		t.Fatalf("authed op reports authed = %v", out["authed"])
	}

	anon := &http.Client{}
	code, anonOut, err := postOpRaw(anon, server, docID, "anonX", `{"base":1,"ops":[{"t":"ins","i":2,"s":"!"}]}`)
	if err != nil {
		t.Fatalf("anon op transport: %v", err)
	}
	if code != http.StatusOK {
		t.Fatalf("anon op status = %d (warn-only must stay open)", code)
	}
	if authed, _ := anonOut["authed"].(bool); authed {
		t.Fatal("anon op reports authed = true")
	}
}

// TestNotesFragmentListsDocs pins the live-index half: a note created
// via /notes/new lands in the store, and the fragment endpoint renders
// its id.
func TestNotesFragmentListsDocs(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)

	docID := createNoteForTest(t, client, server)

	resp := doGet(t, client, server.URL+"/api/notes/fragment")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fragment status = %d", resp.StatusCode)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("fragment read: %v", err)
	}
	if !strings.Contains(buf.String(), docID) {
		t.Fatalf("fragment missing created doc %q", docID)
	}
}

// TestNotesOpCarriesCaret pins atomic text+caret delivery: the caret
// sent with an op batch arrives on the SAME note-text event as the
// resolved text.
func TestNotesOpCarriesCaret(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-opcaret"

	logB, closeB := openNoteStream(t, client, server, docID, "cliB")
	settleStream(t, logB, "cliB")
	defer closeB()

	code, _ := postOp(t, client, server, docID, "cliA",
		`{"base":0,"ops":[{"t":"ins","i":0,"s":"hi"}],"caret":{"line":1,"pos":2}}`)
	if code != http.StatusOK {
		t.Fatalf("op with caret status = %d", code)
	}
	waitNoteEvent(t, logB, func(msg map[string]any) bool {
		if msg["type"] != "note-text" {
			return false
		}
		caret, ok := msg["caret"].(map[string]any)
		if !ok {
			return false
		}
		line, _ := caret["line"].(float64)
		pos, _ := caret["pos"].(float64)
		return line == 1 && pos == 2
	})
}

// TestNotesOpDropsInvalidCaret pins that a malformed caret never poisons
// the op path: the batch still applies, the event just carries no caret.
func TestNotesOpDropsInvalidCaret(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-badcaret"

	logB, closeB := openNoteStream(t, client, server, docID, "cliB")
	settleStream(t, logB, "cliB")
	defer closeB()

	code, out := postOp(t, client, server, docID, "cliA",
		`{"base":0,"ops":[{"t":"ins","i":0,"s":"hi"}],"caret":{"line":0,"pos":-3}}`)
	if code != http.StatusOK || out["text"] != "hi" {
		t.Fatalf("op with bad caret: status = %d, out = %v", code, out)
	}
	msg := waitNoteEvent(t, logB, func(msg map[string]any) bool {
		return msg["type"] == "note-text"
	})
	if _, present := msg["caret"]; present {
		t.Fatalf("invalid caret echoed: %v", msg["caret"])
	}
}

// TestNotesCaretRelay pins line-presence: a caret report reaches peers
// as an ephemeral event carrying the line index.
func TestNotesCaretRelay(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-caret"

	logB, closeB := openNoteStream(t, client, server, docID, "cliB")
	settleStream(t, logB, "cliB")
	defer closeB()

	if code := postPresence(t, client, server, docID, "cliA", "caret", `{"line":5,"pos":17}`); code != http.StatusOK {
		t.Fatalf("caret status = %d", code)
	}
	waitNoteEvent(t, logB, func(msg map[string]any) bool {
		line, _ := msg["x"].(float64)
		pos, _ := msg["y"].(float64)
		return msg["type"] == "caret" && line == 5 && pos == 17
	})
}

// TestNotesTypingBroadcast pins the typing-indicator contract: a typing
// state POST reaches peers as an ephemeral event.
func TestNotesTypingBroadcast(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-typing"

	logB, closeB := openNoteStream(t, client, server, docID, "cliB")
	settleStream(t, logB, "cliB")
	defer closeB()

	if code := postPresence(t, client, server, docID, "cliA", "typing", `{"typing":true}`); code != http.StatusOK {
		t.Fatalf("typing status = %d", code)
	}
	waitNoteEvent(t, logB, func(msg map[string]any) bool {
		return msg["type"] == "typing"
	})
	if code := postPresence(t, client, server, docID, "cliA", "typing", `{"typing":false}`); code != http.StatusOK {
		t.Fatalf("stopped status = %d", code)
	}
	waitNoteEvent(t, logB, func(msg map[string]any) bool {
		return msg["type"] == "stopped"
	})
}

// TestNotesPresenceJoinCount pins the presence pill contract: when C
// joins B's doc, B learns it via join + an authoritative count carrying
// the full peer set.
func TestNotesPresenceJoinCount(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-presence"

	logB, closeB := openNoteStream(t, client, server, docID, "cliB")
	settleStream(t, logB, "cliB")
	defer closeB()
	_, closeC := openNoteStream(t, client, server, docID, "cliC")
	settleStream(t, logB, "cliC")
	defer closeC()

	waitNoteEvent(t, logB, func(msg map[string]any) bool {
		return msg["type"] == "join"
	})
	msg := waitNoteEvent(t, logB, func(msg map[string]any) bool {
		return msg["type"] == "count" && containsAll(peerIDs(msg), "cliB", "cliC")
	})
	if len(peerIDs(msg)) != 2 {
		t.Fatalf("count peers = %v, want exactly {cliB cliC}", peerIDs(msg))
	}
}

// TestNotesPresenceLeave pins the other half: closing C's stream makes B
// observe leave + a count without C (no stale +1).
func TestNotesPresenceLeave(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-leave"

	logB, closeB := openNoteStream(t, client, server, docID, "cliB")
	settleStream(t, logB, "cliB")
	defer closeB()
	_, closeC := openNoteStream(t, client, server, docID, "cliC")
	settleStream(t, logB, "cliC")

	waitNoteEvent(t, logB, func(msg map[string]any) bool {
		return msg["type"] == "count" && containsAll(peerIDs(msg), "cliB", "cliC")
	})
	closeC()

	waitNoteEvent(t, logB, func(msg map[string]any) bool {
		return msg["type"] == "leave"
	})
	msg := waitNoteEvent(t, logB, func(msg map[string]any) bool {
		return msg["type"] == "count" && !containsAll(peerIDs(msg), "cliC")
	})
	if !containsAll(peerIDs(msg), "cliB") {
		t.Fatalf("count after leave = %v, want cliB present", peerIDs(msg))
	}
}

// TestNotesPresenceFallsBackToClientID pins the display-name contract's
// other branch: without auth there is no email, so events carry the raw
// clientID instead of an empty user.
func TestNotesPresenceFallsBackToClientID(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-fallback"

	logB, closeB := openNoteStream(t, client, server, docID, "cliB")
	settleStream(t, logB, "cliB")
	defer closeB()

	anon := &http.Client{}
	if code := postPresence(t, anon, server, docID, "anonX", "typing", `{"typing":true}`); code != http.StatusOK {
		t.Fatalf("anon typing status = %d", code)
	}
	waitNoteEvent(t, logB, func(msg map[string]any) bool {
		return msg["type"] == "typing" && msg["user"] == "anonX"
	})
}
