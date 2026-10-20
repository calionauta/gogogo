// SCOPE:feature — Notes presence + caret handlers port tests

package handlers

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"
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

// TestNotesOpReportsAuthed pins the write contract: an authed op reports
// authed=true, and an ANONYMOUS write is refused with 401 instead of
// being applied. It used to be "warn-only" — the cookie-less client wrote
// text into the doc and persisted a snapshot for it, which is a write by
// an unauthenticated stranger, not a degraded UX. RBAC does not cover this
// (RBAC_ENABLED is opt-in and defaults to false), so the handler owns it.
// Red-proof: drop the session gate from handleOp and the 401 assertion
// goes red while the text lands in the doc.
func TestNotesOpReportsAuthed(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-authedflag"

	_, out := postOp(t, client, server, docID, "cliA", `{"base":0,"ops":[{"t":"ins","i":0,"s":"hi"}]}`)
	if authed, _ := out["authed"].(bool); !authed {
		t.Fatalf("authed op reports authed = %v", out["authed"])
	}

	anon := &http.Client{}
	code, _, err := postOpRaw(anon, server, docID, "anonX", `{"base":1,"ops":[{"t":"ins","i":2,"s":"!"}]}`)
	if err != nil {
		t.Fatalf("anon op transport: %v", err)
	}
	if code != http.StatusUnauthorized {
		t.Fatalf("anon op status = %d, want 401 (anonymous writes are refused)", code)
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

// TestNotesJoinCarriesSenderID pins the per-tab identity on join: the
// event carries the joining tab's clientID in from alongside the display
// user, so peers can key the newcomer without merging same-user tabs.
func TestNotesJoinCarriesSenderID(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-joinfrom"

	logB, closeB := openNoteStream(t, client, server, docID, "cliB")
	settleStream(t, logB, "cliB")
	defer closeB()
	_, closeC := openNoteStream(t, client, server, docID, "cliC")
	settleStream(t, logB, "cliC")
	defer closeC()

	msg := waitNoteEvent(t, logB, func(msg map[string]any) bool {
		return msg["type"] == "join"
	})
	if msg["from"] != "cliC" {
		t.Fatalf("join from = %v, want the joining tab cliC", msg["from"])
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

	msg := waitNoteEvent(t, logB, func(msg map[string]any) bool {
		return msg["type"] == "leave"
	})
	if msg["from"] != "cliC" {
		t.Fatalf("leave from = %v, want the departing tab cliC", msg["from"])
	}
	msg = waitNoteEvent(t, logB, func(msg map[string]any) bool {
		return msg["type"] == "count" && !containsAll(peerIDs(msg), "cliC")
	})
	if !containsAll(peerIDs(msg), "cliB") {
		t.Fatalf("count after leave = %v, want cliB present", peerIDs(msg))
	}
}

// TestNotesSameUserTwoTabsKeepDistinctCarets is the regression guard for
// the "peer caret jumps while I type" report: two tabs signed in as the
// SAME user (same cookie/email, distinct clientIDs) must arrive at a peer
// as two distinguishable senders. Both caret relays and op-attached carets
// carry the per-tab From alongside the shared display User; without From
// the peer keyed both dots by the one email and each tab's heartbeat
// overwrote the other's position.
func TestNotesSameUserTwoTabsKeepDistinctCarets(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-sameuser"

	logB, closeB := openNoteStream(t, client, server, docID, "cliB")
	settleStream(t, logB, "cliB")
	defer closeB()

	// Two tabs, one user: same authed client, distinct clientIDs.
	if code := postPresence(t, client, server, docID, "tabA1", "caret", `{"line":1,"pos":3}`); code != http.StatusOK {
		t.Fatalf("tabA1 caret status = %d", code)
	}
	if code := postPresence(t, client, server, docID, "tabA2", "caret", `{"line":4,"pos":9}`); code != http.StatusOK {
		t.Fatalf("tabA2 caret status = %d", code)
	}
	seen := map[string]map[string]any{}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && len(seen) < 2 {
		// Scan the transcript for caret events (the log is append-only,
		// so re-scanning is cheap and never consumes).
		logB.mu.Lock()
		for _, msg := range logB.events {
			if msg["type"] == "caret" {
				if from, _ := msg["from"].(string); from == "tabA1" || from == "tabA2" {
					seen[from] = msg
				}
			}
		}
		logB.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	if len(seen) != 2 {
		t.Fatalf("peer saw %d distinct caret senders, want tabA1+tabA2", len(seen))
	}
	// Same display user on both (one account), distinct lines (no clobber).
	if seen["tabA1"]["user"] != notesEmail || seen["tabA2"]["user"] != notesEmail {
		t.Fatalf("caret display user wrong: %v / %v", seen["tabA1"]["user"], seen["tabA2"]["user"])
	}
	if seen["tabA1"]["x"] == seen["tabA2"]["x"] {
		t.Fatalf("caret positions merged under one key: %v", seen)
	}

	// Op-attached carets travel the same split: From is the tab, User the
	// account. Post one op per tab (sequential bases) and check both
	// note-text events keep their sender.
	if code, _ := postOp(t, client, server, docID, "tabA1",
		`{"base":0,"ops":[{"t":"ins","i":0,"s":"ab"}],"caret":{"line":1,"pos":2}}`); code != http.StatusOK {
		t.Fatalf("tabA1 op status = %d", code)
	}
	if code, _ := postOp(t, client, server, docID, "tabA2",
		`{"base":1,"ops":[{"t":"ins","i":2,"s":"cd"}],"caret":{"line":1,"pos":4}}`); code != http.StatusOK {
		t.Fatalf("tabA2 op status = %d", code)
	}
	froms := map[string]bool{}
	users := map[string]bool{}
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && len(froms) < 2 {
		logB.mu.Lock()
		for _, msg := range logB.events {
			if msg["type"] == "note-text" {
				if from, _ := msg["from"].(string); from == "tabA1" || from == "tabA2" {
					froms[from] = true
					if user, _ := msg["user"].(string); user != "" {
						users[user] = true
					}
				}
			}
		}
		logB.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	if len(froms) != 2 {
		t.Fatalf("note-text senders = %v, want tabA1+tabA2", froms)
	}
	// Display name still travels with the tab id: one account, two tabs.
	if len(users) != 1 || !users[notesEmail] {
		t.Fatalf("note-text display users = %v, want only %q", users, notesEmail)
	}
}

// TestNotesPresenceFallsBackToClientID pins the display-name contract's
// other branch AND the presence auth rule: an anonymous caller is refused
// (401) instead of painting a raw id on live peers. The fallback branch
// itself lives in collab.DisplayName (empty email -> raw client id) and is
// pinned by internal/collab's own tests; a session always carries an
// email, so the handler cannot reach it any more.
func TestNotesPresenceFallsBackToClientID(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-fallback"

	logB, closeB := openNoteStream(t, client, server, docID, "cliB")
	settleStream(t, logB, "cliB")
	defer closeB()

	// The fallback name is only reachable by a SESSION whose user carries no
	// email-equivalent — an anonymous caller is refused outright (401), so
	// the raw-clientID branch must be driven by a signed-in tab that
	// deliberately sends no display name.
	anon := &http.Client{}
	code := postPresence(t, anon, server, docID, "anonX", "typing", `{"typing":true}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("anon typing status = %d, want 401 (presence is signed-in only)", code)
	}
}
