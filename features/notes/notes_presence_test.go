// SCOPE:layer=feature,removal=feature — Notes stream + presence tests.
package notes_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
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

// postTyping sends a typing state as clientID.
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
		return msg["type"] == "typing" && msg["user"] == "cliA"
	})

	if code := postTyping(t, client, baseURL, docID, "cliA", false); code != http.StatusOK {
		t.Fatalf("stopped status = %d", code)
	}
	waitNoteEvent(t, gotB, func(msg map[string]any) bool {
		return msg["type"] == "stopped" && msg["user"] == "cliA"
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
		return msg["type"] == "join" && msg["user"] == "cliC"
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
		return msg["type"] == "leave" && msg["user"] == "cliC"
	})
	msg := waitNoteEvent(t, gotB, func(msg map[string]any) bool {
		return msg["type"] == "count" && !containsAll(peerIDs(msg), "cliC")
	})
	if !containsAll(peerIDs(msg), "cliB") {
		t.Fatalf("count after leave = %v, want cliB present", peerIDs(msg))
	}
}
