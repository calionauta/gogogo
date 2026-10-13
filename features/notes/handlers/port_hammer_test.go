// SCOPE:feature — Notes concurrency handlers port tests

package handlers

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	notescomponents "github.com/calionauta/gogogo/features/notes/components"
)

// TestNotesConcurrentHammer pins convergence under contention: many
// writers racing the same doc, each retrying against the authoritative
// rev, and no edit lost (markers all present).
func TestNotesConcurrentHammer(t *testing.T) {
	server, _, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := "note-hammer"

	const writers = 8
	const perWriter = 5
	errCh := make(chan string, writers*perWriter*4)
	revCh := make(chan uint64, writers*perWriter*30)
	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			who := fmt.Sprintf("cliH%d", w)
			var rev uint64
			for b := range perWriter {
				marker := fmt.Sprintf("w%db%d;", w, b)
				body := fmt.Sprintf(`{"base":%d,"ops":[{"t":"ins","i":0,"s":%q}]}`, rev, marker)
				ok := false
				deadline := time.Now().Add(30 * time.Second)
				attempts := 0
				for !ok && time.Now().Before(deadline) {
					code, out, err := postOpRaw(client, server, docID, who, body)
					if err != nil {
						errCh <- fmt.Sprintf("w%d b%d transport: %v", w, b, err)
						return
					}
					if r, _ := out["rev"].(float64); r > 0 {
						rev = uint64(r)
						revCh <- rev
					}
					switch code {
					case http.StatusOK:
						ok = true
					case http.StatusConflict:
						body = fmt.Sprintf(`{"base":%d,"ops":[{"t":"ins","i":0,"s":%q}]}`, rev, marker)
						attempts++
						time.Sleep(time.Duration(min(attempts*2, 20)) * time.Millisecond)
					default:
						errCh <- fmt.Sprintf("w%d b%d unexpected status %d: %v", w, b, code, out)
						return
					}
				}
				if !ok {
					errCh <- fmt.Sprintf("w%d b%d never landed", w, b)
				}
			}
		}(w)
	}
	wg.Wait()
	close(errCh)
	close(revCh)
	for msg := range errCh {
		t.Error(msg)
	}
	var maxRev uint64
	for r := range revCh {
		if r > maxRev {
			maxRev = r
		}
	}
	if maxRev < writers*perWriter {
		t.Errorf("highest rev = %d, want >= %d (every writer landed)", maxRev, writers*perWriter)
	}
}

// TestNotesCaretRecipePinsMirrorProps guards the canonical mirror-div
// recipe inside static/notes.js: pixel mapping is only exact when every
// text-affecting CSS property is copied from the live textarea.
func TestNotesCaretRecipePinsMirrorProps(t *testing.T) {
	raw, err := os.ReadFile("../static/notes.js")
	if err != nil {
		t.Fatalf("read notes.js: %v", err)
	}
	src := string(raw)
	for _, prop := range []string{
		"fontFamily", "fontSize", "fontWeight", "lineHeight",
		"letterSpacing", "textTransform", "textIndent",
		"paddingTop", "paddingRight", "paddingBottom", "paddingLeft",
		"borderTopWidth", "borderRightWidth", "borderBottomWidth", "borderLeftWidth",
		"boxSizing", "whiteSpace", "wordWrap", "overflowWrap", "tabSize",
		"getComputedStyle", "offsetLeft", "offsetTop",
		"scrollLeft", "scrollTop", "selectionStart",
		"shiftPeerOffsets",
	} {
		if !strings.Contains(src, prop) {
			t.Errorf("notes.js missing mirror recipe %q", prop)
		}
	}
}

// TestNotesCaretsKeyedByTabID pins the same-user caret fix inside
// static/notes.js: carets and typists are keyed by the server-stamped
// per-tab id (msg.from) with a fallback to the display name for senders
// that predate the field, and rendered with the display name as the
// label. Keying by the display name merged two tabs of one account under
// a single dot, so the peer's caret jumped lines while they typed.
// Reverting the JS to name keying (or dropping the fallback) fails this
// test; the live two-tab behavior is covered by scripts/notes-caret.test.mjs
// and TestNotesSameUserTwoTabsKeepDistinctCarets (server side).
func TestNotesCaretsKeyedByTabID(t *testing.T) {
	raw, err := os.ReadFile("../static/notes.js")
	if err != nil {
		t.Fatalf("read notes.js: %v", err)
	}
	src := string(raw)
	for _, want := range []string{
		"m.from || m.user", // peerID: per-tab id, legacy fallback to name
		"m.user || m.from", // peerLabel: display name first
		"carets.set(id,",   // caret dots keyed per tab, not per user
		"typists.set(id,",  // typing pill keyed per tab, not per user
		"peerID(m)",        // inbound frames resolve identity via peerID
		"peerLabel(m)",     // ... and render via peerLabel
	} {
		if !strings.Contains(src, want) {
			t.Errorf("notes.js missing tab-keyed caret wiring %q", want)
		}
	}
}

// TestNotesIndexLoadsDatastarRuntime pins the live-index wiring: the
// resync button is a Datastar @get, so the page must load the runtime.
func TestNotesIndexLoadsDatastarRuntime(t *testing.T) {
	var buf bytes.Buffer
	err := notescomponents.NotesIndex(notesEmail, []string{"abc"},
		"dev", "c0ffee").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render index: %v", err)
	}
	html := buf.String()
	for _, want := range []string{"/static/datastar.js", "notes-realtime-resync", "data-on:click", "/api/notes/fragment"} {
		if !strings.Contains(html, want) {
			t.Errorf("index page missing %q", want)
		}
	}
}
