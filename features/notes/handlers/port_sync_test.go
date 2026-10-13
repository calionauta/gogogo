// SCOPE:feature — Notes sync-protocol handlers port tests

package handlers

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

// TestNotesOpRoundTrip pins the core contract: insert ops resolve to
// text, later ops append, and the page renders the resolved state.
func TestNotesOpRoundTrip(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-roundtrip"

	code, out := postOp(t, client, server, docID, "cliA", `{"ops":[{"t":"ins","i":0,"s":"hello"}]}`)
	if code != http.StatusOK || out["text"] != "hello" {
		t.Fatalf("op status = %d, out = %v", code, out)
	}
	code, out = postOp(t, client, server, docID, "cliA", `{"base":1,"ops":[{"t":"ins","i":5,"s":" world"}]}`)
	if code != http.StatusOK || out["text"] != "hello world" {
		t.Fatalf("append: status = %d, out = %v", code, out)
	}

	resp := doGet(t, client, server.URL+"/notes/"+docID)
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("page read: %v", err)
	}
	if !strings.Contains(buf.String(), "hello world") {
		t.Fatal("page does not contain resolved text")
	}
}

// TestNotesDelete pins delete semantics with clamping.
func TestNotesDelete(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-delete"

	postOp(t, client, server, docID, "cliA", `{"base":0,"ops":[{"t":"ins","i":0,"s":"hello world"}]}`)
	_, out := postOp(t, client, server, docID, "cliA", `{"base":1,"ops":[{"t":"del","i":5,"n":99}]}`)
	if out["text"] != "hello" {
		t.Fatalf("delete clamped: text = %v", out["text"])
	}
}

// TestNotesOpValidation pins rejection of malformed ops.
func TestNotesOpValidation(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-bad"

	if code, _ := postOp(t, client, server, docID, "cliA",
		`{"ops":[{"t":"explode","i":0}]}`); code != http.StatusBadRequest {
		t.Fatalf("unknown op type: status = %d, want 400", code)
	}
	if code, _ := postOp(t, client, server, docID, "cliA", `not json`); code != http.StatusBadRequest {
		t.Fatalf("bad json: status = %d, want 400", code)
	}
}

// TestNotesPersistsSnapshot pins that the resolved snapshot survives the
// op path through the Persister interface.
func TestNotesPersistsSnapshot(t *testing.T) {
	server, persister, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-snap"

	postOp(t, client, server, docID, "cliA", `{"ops":[{"t":"ins","i":0,"s":"persist me"}]}`)

	snap, ok := persister.LoadSnapshot(docID)
	if !ok || len(snap) == 0 {
		t.Fatalf("no snapshot persisted for %q", docID)
	}
}

// TestNotesStreamReceivesText pins live fan-out: B's open stream must
// receive A's op as a note-text event.
func TestNotesStreamReceivesText(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-stream"

	logB, closeB := openNoteStream(t, client, server, docID, "cliB")
	settleStream(t, logB, "cliB")
	defer closeB()

	postOp(t, client, server, docID, "cliA", `{"base":0,"ops":[{"t":"ins","i":0,"s":"live text"}]}`)

	waitNoteEvent(t, logB, func(msg map[string]any) bool {
		return msg["type"] == "note-text" && msg["text"] == "live text"
	})
}

// TestNotesInsertClampsToEnd pins append-on-overflow instead of an error.
func TestNotesInsertClampsToEnd(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-clamp"

	_, out := postOp(t, client, server, docID, "cliA", `{"base":0,"ops":[{"t":"ins","i":999,"s":"hi"}]}`)
	if out["text"] != "hi" {
		t.Fatalf("clamped insert: text = %v", out["text"])
	}
}

// TestNotesStaleBaseRejected pins the anti-loss contract: an op batch
// computed against an old base must NOT apply — 409 with the
// authoritative text + revision so the client re-syncs.
func TestNotesStaleBaseRejected(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-stale"

	code, out := postOp(t, client, server, docID, "cliA", `{"base":0,"ops":[{"t":"ins","i":0,"s":"hello"}]}`)
	if code != http.StatusOK {
		t.Fatalf("first op: status = %d: %v", code, out)
	}
	code, out = postOp(t, client, server, docID, "cliB", `{"base":0,"ops":[{"t":"ins","i":0,"s":"XYZ "}]}`)
	if code != http.StatusConflict {
		t.Fatalf("stale base: status = %d, want 409", code)
	}
	if out["text"] != "hello" {
		t.Fatalf("stale base: authoritative text = %v, want hello", out["text"])
	}
	if rev, _ := out["rev"].(float64); rev != 1 {
		t.Fatalf("stale base: rev = %v, want 1", out["rev"])
	}
}

// TestNotesConcurrentBatchesMerge pins convergence through the
// reject-and-retry loop: B's stale batch is refused, B replays against
// the fresh base, both edits survive.
func TestNotesConcurrentBatchesMerge(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-race"

	postOp(t, client, server, docID, "cliA", `{"base":0,"ops":[{"t":"ins","i":0,"s":"hello"}]}`)
	code, _ := postOp(t, client, server, docID, "cliB", `{"base":0,"ops":[{"t":"ins","i":0,"s":"XYZ "}]}`)
	if code != http.StatusConflict {
		t.Fatalf("race: stale batch status = %d, want 409", code)
	}
	_, out := postOp(t, client, server, docID, "cliB", `{"base":1,"ops":[{"t":"ins","i":0,"s":"XYZ "}]}`)
	text, _ := out["text"].(string)
	if !strings.Contains(text, "hello") || !strings.Contains(text, "XYZ") {
		t.Fatalf("replayed merge lost an edit: %q", text)
	}
	if rev, _ := out["rev"].(float64); rev != 2 {
		t.Fatalf("replayed merge: rev = %v, want 2", out["rev"])
	}
}

// TestNotesRevChainMonotonic pins the happy path a well-behaved client
// walks: every batch carries the base learned from the previous answer.
func TestNotesRevChainMonotonic(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-chain"

	want := []struct {
		body string
		rev  float64
		text string
	}{
		{`{"base":0,"ops":[{"t":"ins","i":0,"s":"a"}]}`, 1, "a"},
		{`{"base":1,"ops":[{"t":"ins","i":1,"s":"b"}]}`, 2, "ab"},
		{`{"base":2,"ops":[{"t":"ins","i":2,"s":"c"}]}`, 3, "abc"},
	}
	for _, w := range want {
		code, out := postOp(t, client, server, docID, "cliA", w.body)
		if code != http.StatusOK {
			t.Fatalf("chain op %v: status = %d", w.body, code)
		}
		if rev, _ := out["rev"].(float64); rev != w.rev {
			t.Fatalf("chain op %v: rev = %v, want %v", w.body, rev, w.rev)
		}
		if out["text"] != w.text {
			t.Fatalf("chain op %v: text = %v, want %v", w.body, out["text"], w.text)
		}
	}
}

// TestNotesStreamCarriesRev pins passive base advancement: a client that
// only listens learns the current revision from stream events.
func TestNotesStreamCarriesRev(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-streamrev"

	gotB, closeB := openNoteStream(t, client, server, docID, "cliB")
	defer closeB()

	postOp(t, client, server, docID, "cliA", `{"base":0,"ops":[{"t":"ins","i":0,"s":"hi"}]}`)
	msg := waitNoteEvent(t, gotB, func(msg map[string]any) bool {
		return msg["type"] == "note-text"
	})
	if rev, _ := msg["rev"].(float64); rev < 1 {
		t.Fatalf("stream event rev = %v, want >= 1", msg["rev"])
	}
	code, out := postOp(t, client, server, docID, "cliB", `{"base":1,"ops":[{"t":"ins","i":2,"s":"!"}]}`)
	if code != http.StatusOK {
		t.Fatalf("informed append: status = %d: %v", code, out)
	}
	if out["text"] != "hi!" {
		t.Fatalf("informed append: text = %v, want hi!", out["text"])
	}
}
