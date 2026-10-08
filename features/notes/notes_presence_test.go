// SCOPE:layer=feature,removal=feature — Notes stream + presence tests.
package notes_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
)

func openNoteStream(
	t *testing.T, client *http.Client, baseURL, docID, clientID string,
) (chan string, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		baseURL+"/api/notes/"+docID+"/stream?clientID="+clientID, nil)
	if err != nil {
		cancel()
		t.Fatalf("stream req: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("stream open: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		cancel()
		resp.Body.Close()
		t.Fatalf("stream status = %d", resp.StatusCode)
	}
	got := make(chan string, 32)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			if rest, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				got <- rest
			}
		}
	}()
	return got, func() { cancel(); resp.Body.Close() }
}

// waitNoteEvent returns the first payload matching pred, or fails after
// 10s.
func waitNoteEvent(t *testing.T, got chan string, pred func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case data := <-got:
			var msg map[string]any
			if err := json.Unmarshal([]byte(data), &msg); err != nil {
				continue
			}
			if pred(msg) {
				return msg
			}
		case <-deadline:
			t.Fatalf("timed out waiting for matching event")
			return nil
		}
	}
}

// peerIDs extracts the peers list from a count/join event.
func peerIDs(msg map[string]any) []string {
	var out []string
	if peers, ok := msg["peers"].([]any); ok {
		for _, p := range peers {
			if s, ok := p.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func containsAll(hay []string, needles ...string) bool {
	set := map[string]bool{}
	for _, h := range hay {
		set[h] = true
	}
	for _, n := range needles {
		if !set[n] {
			return false
		}
	}
	return true
}

// TestNotesOpCarriesCaret pins atomic text+caret delivery: the caret sent
// with an op batch arrives on the SAME note-text event as the resolved
// text — no separate-channel race where the dot lands before its letters
// exist (the "line below, then corrects" class).
func TestNotesOpCarriesCaret(t *testing.T) {
	baseURL, _, cleanup := notesFixture(t)
	defer cleanup()
	client := notesAuthedClient(t, baseURL)
	docID := "note-opcaret"

	gotB, closeB := openNoteStream(t, client, baseURL, docID, "cliB")
	defer closeB()

	code, _ := postOp(t, client, baseURL, docID, "cliA",
		`{"base":0,"ops":[{"t":"ins","i":0,"s":"hi"}],"caret":{"line":1,"pos":2}}`)
	if code != http.StatusOK {
		t.Fatalf("op with caret status = %d", code)
	}
	waitNoteEvent(t, gotB, func(msg map[string]any) bool {
		if msg["type"] != "note-text" {
			return false
		}
		caret, ok := msg["caret"].(map[string]any)
		if !ok {
			return false
		}
		line, _ := caret["line"].(float64)
		pos, _ := caret["pos"].(float64)
		return msg["from"] == notesEmail && line == 1 && pos == 2
	})
}

// TestNotesOpDropsInvalidCaret pins that a malformed caret never poisons
// the op path: the batch still applies, the event just carries no caret.
func TestNotesOpDropsInvalidCaret(t *testing.T) {
	baseURL, _, cleanup := notesFixture(t)
	defer cleanup()
	client := notesAuthedClient(t, baseURL)
	docID := "note-badcaret"

	gotB, closeB := openNoteStream(t, client, baseURL, docID, "cliB")
	defer closeB()

	code, out := postOp(t, client, baseURL, docID, "cliA",
		`{"base":0,"ops":[{"t":"ins","i":0,"s":"hi"}],"caret":{"line":0,"pos":-3}}`)
	if code != http.StatusOK || out["text"] != "hi" {
		t.Fatalf("op with bad caret: status = %d, out = %v", code, out)
	}
	msg := waitNoteEvent(t, gotB, func(msg map[string]any) bool {
		return msg["type"] == "note-text"
	})
	if _, present := msg["caret"]; present {
		t.Fatalf("invalid caret echoed: %v", msg["caret"])
	}
}

// postCaret sends a caret report as clientID: line index (dimension-
// independent) + UTF-16 offset (for pixel mapping on the peer's own
// viewport via mirror-div — each browser measures itself, so zoom, fonts
// and wrapping never desync the math).
func postCaret(t *testing.T, client *http.Client, baseURL, docID, clientID string, line, pos int) int {
	t.Helper()
	body := fmt.Sprintf(`{"line":%d,"pos":%d}`, line, pos)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		baseURL+"/api/notes/"+docID+"/caret?clientID="+clientID,
		strings.NewReader(body))
	if err != nil {
		t.Fatalf("caret req: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("caret post: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// TestNotesCaretRelay pins line-presence: a caret report reaches peers
// as an ephemeral event carrying the line index (no persistence, no
// NATS — liveness only, like typing).
func TestNotesCaretRelay(t *testing.T) {
	baseURL, _, cleanup := notesFixture(t)
	defer cleanup()
	client := notesAuthedClient(t, baseURL)
	docID := "note-caret"

	gotB, closeB := openNoteStream(t, client, baseURL, docID, "cliB")
	defer closeB()

	if code := postCaret(t, client, baseURL, docID, "cliA", 5, 17); code != http.StatusOK {
		t.Fatalf("caret status = %d", code)
	}
	waitNoteEvent(t, gotB, func(msg map[string]any) bool {
		line, _ := msg["x"].(float64)
		pos, _ := msg["y"].(float64)
		return msg["type"] == "caret" && msg["user"] == notesEmail && line == 5 && pos == 17
	})
}

// postTyping sends a typing state as clientID (any client: authed or
// anonymous — the server falls back to the raw id without auth).
func postTyping(t *testing.T, client *http.Client, baseURL, docID, clientID string, typing bool) int {
	t.Helper()
	body := `{"typing":false}`
	if typing {
		body = `{"typing":true}`
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		baseURL+"/api/notes/"+docID+"/typing?clientID="+clientID,
		strings.NewReader(body))
	if err != nil {
		t.Fatalf("typing req: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("typing post: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// TestNotesTypingBroadcast pins the typing-indicator contract: a typing
// state POST reaches peers as an ephemeral event (no persistence, no
// NATS — same-instance liveness only, like cursor presence).
func TestNotesTypingBroadcast(t *testing.T) {
	baseURL, _, cleanup := notesFixture(t)
	defer cleanup()
	client := notesAuthedClient(t, baseURL)
	docID := "note-typing"

	gotB, closeB := openNoteStream(t, client, baseURL, docID, "cliB")
	defer closeB()

	if code := postTyping(t, client, baseURL, docID, "cliA", true); code != http.StatusOK {
		t.Fatalf("typing status = %d", code)
	}
	waitNoteEvent(t, gotB, func(msg map[string]any) bool {
		return msg["type"] == "typing" && msg["user"] == notesEmail
	})

	if code := postTyping(t, client, baseURL, docID, "cliA", false); code != http.StatusOK {
		t.Fatalf("stopped status = %d", code)
	}
	waitNoteEvent(t, gotB, func(msg map[string]any) bool {
		return msg["type"] == "stopped" && msg["user"] == notesEmail
	})
}

// TestNotesPresenceJoinCount pins the presence pill contract: when C
// joins B's doc, B learns it via join + an authoritative count carrying
// the full peer set (no client-side counting).
func TestNotesPresenceJoinCount(t *testing.T) {
	baseURL, _, cleanup := notesFixture(t)
	defer cleanup()
	client := notesAuthedClient(t, baseURL)
	docID := "note-presence"

	gotB, closeB := openNoteStream(t, client, baseURL, docID, "cliB")
	defer closeB()
	_, closeC := openNoteStream(t, client, baseURL, docID, "cliC")
	defer closeC()

	waitNoteEvent(t, gotB, func(msg map[string]any) bool {
		return msg["type"] == "join" && msg["user"] == notesEmail
	})
	msg := waitNoteEvent(t, gotB, func(msg map[string]any) bool {
		return msg["type"] == "count" && containsAll(peerIDs(msg), "cliB", "cliC")
	})
	if len(peerIDs(msg)) != 2 {
		t.Fatalf("count peers = %v, want exactly {cliB cliC}", peerIDs(msg))
	}
}

// TestNotesPresenceLeave pins the other half: closing C's stream makes B
// observe leave + a count without C (no stale +1).
func TestNotesPresenceLeave(t *testing.T) {
	baseURL, _, cleanup := notesFixture(t)
	defer cleanup()
	client := notesAuthedClient(t, baseURL)
	docID := "note-leave"

	gotB, closeB := openNoteStream(t, client, baseURL, docID, "cliB")
	defer closeB()
	_, closeC := openNoteStream(t, client, baseURL, docID, "cliC")

	waitNoteEvent(t, gotB, func(msg map[string]any) bool {
		return msg["type"] == "count" && containsAll(peerIDs(msg), "cliB", "cliC")
	})
	closeC()

	waitNoteEvent(t, gotB, func(msg map[string]any) bool {
		return msg["type"] == "leave" && msg["user"] == notesEmail
	})
	msg := waitNoteEvent(t, gotB, func(msg map[string]any) bool {
		return msg["type"] == "count" && !containsAll(peerIDs(msg), "cliC")
	})
	if !containsAll(peerIDs(msg), "cliB") {
		t.Fatalf("count after leave = %v, want cliB present", peerIDs(msg))
	}
}

// TestNotesStreamReceivesText pins live fan-out: B's open stream must
// receive A's op as a note-text event (exclude-origin: A gets nothing).
func TestNotesStreamReceivesText(t *testing.T) {
	baseURL, _, cleanup := notesFixture(t)
	defer cleanup()
	client := notesAuthedClient(t, baseURL)
	docID := "note-stream"

	gotB, closeB := openNoteStream(t, client, baseURL, docID, "cliB")
	defer closeB()

	postOp(t, client, baseURL, docID, "cliA", `{"base":0,"ops":[{"t":"ins","i":0,"s":"live text"}]}`)

	waitNoteEvent(t, gotB, func(msg map[string]any) bool {
		return msg["type"] == "note-text" && msg["text"] == "live text" && msg["from"] == notesEmail
	})
}

// TestNotesInsertClampsToEnd pins append-on-overflow instead of an error:
// positions shift under concurrency, so the server serializes with clamps.
func TestNotesInsertClampsToEnd(t *testing.T) {
	baseURL, _, cleanup := notesFixture(t)
	defer cleanup()
	client := notesAuthedClient(t, baseURL)
	docID := "note-clamp"

	_, out := postOp(t, client, baseURL, docID, "cliA", `{"base":0,"ops":[{"t":"ins","i":999,"s":"hi"}]}`)
	if out["text"] != "hi" {
		t.Fatalf("clamped insert: text = %v", out["text"])
	}
}

// TestNotesPresenceFallsBackToClientID pins the display-name contract's
// other branch: without auth there is no email, so events carry the raw
// clientID instead of an empty user.
func TestNotesPresenceFallsBackToClientID(t *testing.T) {
	baseURL, _, cleanup := notesFixture(t)
	defer cleanup()
	client := notesAuthedClient(t, baseURL)
	docID := "note-fallback"

	gotB, closeB := openNoteStream(t, client, baseURL, docID, "cliB")
	defer closeB()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	anon := &http.Client{Jar: jar}
	if code := postTyping(t, anon, baseURL, docID, "anonX", true); code != http.StatusOK {
		t.Fatalf("anon typing status = %d", code)
	}
	waitNoteEvent(t, gotB, func(msg map[string]any) bool {
		return msg["type"] == "typing" && msg["user"] == "anonX"
	})
}
