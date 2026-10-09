// SCOPE:feature - Whiteboard session-frame test.
package whiteboard_test

import (
	"strings"
	"testing"
)

// TestWhiteboard_StreamStartsWithSessionFrame pins the shared session
// contract on the board stream: the connection's auth state arrives as a
// frame so the browser can show the session banner instead of silently
// degrading peer cursors to raw client ids. Same collab.SessionEvent the
// notes stream sends — one wire contract for every collab stream.
func TestWhiteboard_StreamStartsWithSessionFrame(t *testing.T) {
	t.Parallel()
	baseURL, _, cleanup := webFixture(t)
	defer cleanup()

	client := newWBClient(t)
	login(t, client, baseURL)

	docID := "doc-session-frame"
	s := openWBStream(t, client, baseURL, docID, "wbSess")
	defer s.close()

	evs := s.waitFor(func(ev string) bool { return strings.Contains(ev, `"type":"session"`) })
	var frame string
	for _, ev := range evs {
		if strings.Contains(ev, `"type":"session"`) {
			frame = ev
			break
		}
	}
	if frame == "" {
		t.Fatalf("no session frame on the board stream; events=%v", evs)
	}
	if !strings.Contains(frame, `"authed":true`) {
		t.Fatalf("session frame not authed: %q", frame)
	}
}
