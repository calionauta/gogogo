// SCOPE:feature — Whiteboard handlers ported contracts (ported from the legacy whiteboard suite)

package handlers

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/collab"
)

// TestWhiteboard_StreamStartsWithSessionFrame pins the shared session
// contract on the board stream: the connection's auth state arrives as a
// frame so the browser can show the session banner instead of silently
// degrading peer cursors to raw client ids.
func TestWhiteboard_StreamStartsWithSessionFrame(t *testing.T) {
	server, _, cookie := wbPortFixture(t)
	client := wbClient(t, server, cookie)

	docID := "doc-session-frame"
	s := openWBStream(t, client, server, docID, "wbSess")
	defer s.close()

	evs := s.waitFor(func(ev string) bool { return strings.Contains(ev, `"type":"session"`) })
	for _, ev := range evs {
		if !strings.Contains(ev, `"authed":true`) {
			continue
		}
		return
	}
	t.Fatalf("no authed session frame on the board stream; events=%v", tailEvents(evs, 400))
}

// TestWhiteboard_StaleOpReturns409 pins the lost-update guard: a stale
// op must be answered 409 plus the authoritative shapes, and the
// winning edit survives.
func TestWhiteboard_StaleOpReturns409(t *testing.T) {
	server, _, cookie := wbPortFixture(t)
	client := wbClient(t, server, cookie)
	docID := "doc-stale-port"

	post := func(op collab.ShapeOp) *http.Response {
		resp := postOp(t, client, server, docID, "wb1", op)
		return resp
	}

	resp := post(collab.ShapeOp{Op: "add", Shape: collab.Shape{ID: "s", Type: "rect", X: 0}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initial draw status = %d, want 200", resp.StatusCode)
	}
	var okBody struct {
		Shapes []collab.Shape `json:"shapes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&okBody); err != nil {
		t.Fatalf("decode 200 body: %v", err)
	}
	resp.Body.Close()
	if len(okBody.Shapes) != 1 || okBody.Shapes[0].Version != 1 {
		t.Fatalf("200 body must carry the versioned shape, got %+v", okBody.Shapes)
	}

	resp = post(collab.ShapeOp{Op: "add", Shape: collab.Shape{ID: "s", Type: "rect", X: 99}, BaseVersion: 1})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("versioned edit status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	resp = post(collab.ShapeOp{Op: "add", Shape: collab.Shape{ID: "s", Type: "rect", X: 10}, BaseVersion: 1})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("stale op status = %d, want 409", resp.StatusCode)
	}
	var conflict struct {
		Error  string         `json:"error"`
		Shapes []collab.Shape `json:"shapes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&conflict); err != nil {
		t.Fatalf("decode 409 body: %v", err)
	}
	resp.Body.Close()
	if conflict.Error != "stale" {
		t.Errorf("409 error = %q, want \"stale\"", conflict.Error)
	}
	if len(conflict.Shapes) != 1 || conflict.Shapes[0].X != 99 {
		t.Errorf("409 body must carry current state (x=99), got %+v", conflict.Shapes)
	}
	shapes := whiteboardShapes(t, client, server, docID)
	if len(shapes) != 1 || shapes[0].X != 99 {
		t.Fatalf("peer's x=99 was clobbered by the stale op: %+v", shapes)
	}
}

// TestWhiteboard_DrawAfterClearSucceeds pins that clearing a board does
// not leave stale versions behind: drawing again must be accepted.
func TestWhiteboard_DrawAfterClearSucceeds(t *testing.T) {
	server, _, cookie := wbPortFixture(t)
	client := wbClient(t, server, cookie)
	docID := "doc-cleardraw-port"

	postOp(t, client, server, docID, "wb1",
		collab.ShapeOp{Op: "add", Shape: collab.Shape{ID: "a", Type: "rect"}}).Body.Close()
	postOp(t, client, server, docID, "wb1", collab.ShapeOp{Op: "clear", BaseVersion: 1}).Body.Close()

	resp := postOp(t, client, server, docID, "wb1",
		collab.ShapeOp{Op: "add", Shape: collab.Shape{ID: "b", Type: "rect", X: 5}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("draw after clear status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()
	shapes := whiteboardShapes(t, client, server, docID)
	if len(shapes) != 1 || shapes[0].ID != "b" {
		t.Fatalf("shapes after clear+draw = %+v, want exactly b", shapes)
	}
}

// TestWhiteboard_ShapeBroadcastAndPersist pins broadcast + persistence +
// no-echo: A draws, B's stream receives the shape, A's own stream does
// not, and the persister holds the snapshot.
func TestWhiteboard_ShapeBroadcastAndPersist(t *testing.T) {
	server, persister, cookieA := wbPortFixture(t)
	cookieB := wbSignup(t, server.Config.Handler, "peerb@example.com")
	clientA := wbClient(t, server, cookieA)
	clientB := wbClient(t, server, cookieB)
	docID := "doc-broadcast-port"

	streamA := openWBStream(t, clientA, server, docID, "wbA")
	streamB := openWBStream(t, clientB, server, docID, "wbB")
	defer streamA.close()
	defer streamB.close()
	streamA.settleJoin()
	streamB.settleJoin()

	postOp(t, clientA, server, docID, "wbA",
		collab.ShapeOp{Op: "add", Shape: collab.Shape{ID: "shape-1", Type: "rect", X: 7}}).Body.Close()

	evsB := streamB.waitFor(func(ev string) bool { return shapesEventContains([]string{ev}, "shape-1") })
	if !shapesEventContains(evsB, "shape-1") {
		t.Fatalf("peer never received the shape; events=%s", tailEvents(evsB, 400))
	}
	// No-echo: A's own stream must not carry its shape back.
	echo := streamA.drain(300 * time.Millisecond)
	if shapesEventContains(echo, "shape-1") {
		t.Fatal("originator received its own shape echoed (no-echo violated)")
	}
	snap, ok := persister.LoadSnapshot(docID)
	if !ok || len(snap) == 0 {
		t.Fatal("persister holds no snapshot after the draw")
	}
}

// TestWhiteboard_PresenceBroadcast pins presence relay: a cursor POST
// from A lands on B's stream.
func TestWhiteboard_PresenceBroadcast(t *testing.T) {
	server, _, cookieA := wbPortFixture(t)
	cookieB := wbSignup(t, server.Config.Handler, "presb@example.com")
	clientA := wbClient(t, server, cookieA)
	clientB := wbClient(t, server, cookieB)
	docID := "doc-presence-port"

	streamA := openWBStream(t, clientA, server, docID, "wbA")
	streamB := openWBStream(t, clientB, server, docID, "wbB")
	defer streamA.close()
	defer streamB.close()
	streamA.settleJoin()
	streamB.settleJoin()

	body, err := json.Marshal(collab.PresenceMsg{Type: "cursor", Doc: docID, User: "wbA", X: 0.25, Y: 0.75, TS: 1})
	if err != nil {
		t.Fatalf("marshal cursor: %v", err)
	}
	resp, err := postWithClientID(context.Background(), clientA,
		server.URL+"/api/whiteboard/"+docID+"/presence", "wbA", body)
	if err != nil {
		t.Fatalf("post cursor: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post cursor status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	evs := streamB.waitFor(func(ev string) bool { _, ok := cursorFromEvents([]string{ev}); return ok })
	cur, ok := cursorFromEvents(evs)
	if !ok {
		t.Fatalf("peer never received the cursor; events=%s", tailEvents(evs, 400))
	}
	if cur.User != wbEmail {
		t.Fatalf("cursor user = %q, want the server-stamped %q (anti-spoof)", cur.User, wbEmail)
	}
	if math.Abs(cur.X-0.25) > 1e-6 || math.Abs(cur.Y-0.75) > 1e-6 {
		t.Fatalf("cursor coords = (%v,%v), want (0.25,0.75)", cur.X, cur.Y)
	}
}

// TestWhiteboard_PresenceToleratesStringCoords pins the tolerant
// decoder: stringified coords (a JS fork) must not 400 the presence
// POST, and number coords keep working.
func TestWhiteboard_PresenceToleratesStringCoords(t *testing.T) {
	server, _, cookie := wbPortFixture(t)
	client := wbClient(t, server, cookie)
	docID := "doc-presstr-port"
	presenceURL := server.URL + "/api/whiteboard/" + docID + "/presence"

	strBody := []byte(`{"type":"cursor","doc":"` + docID + `","user":"u-str",` +
		`"x":"0.1234","y":"0.4321","ts":1,"cts":"1700000000000"}`)
	resp, err := postWithClientID(context.Background(), client, presenceURL, "wb-str", strBody)
	if err != nil {
		t.Fatalf("presence (string) POST: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("presence (string coords) should be 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	numBody, err := json.Marshal(collab.PresenceMsg{Type: "cursor", Doc: docID, User: "u-num", X: 0.5, Y: 0.5, TS: 1})
	if err != nil {
		t.Fatalf("marshal num presence: %v", err)
	}
	resp2, err := postWithClientID(context.Background(), client, presenceURL, "wb-num", numBody)
	if err != nil {
		t.Fatalf("presence (number) POST: %v", err)
	}
	if resp2.StatusCode != http.StatusOK {
		resp2.Body.Close()
		t.Fatalf("presence (number coords) should be 200, got %d", resp2.StatusCode)
	}
	resp2.Body.Close()
}

// TestWhiteboard_PeerCountAuthoritative pins the server-owned peer set:
// both clients learn the same authoritative count of 2, each seeing the
// other's client id.
func TestWhiteboard_PeerCountAuthoritative(t *testing.T) {
	server, _, cookieA := wbPortFixture(t)
	cookieB := wbSignup(t, server.Config.Handler, "countb@example.com")
	clientA := wbClient(t, server, cookieA)
	clientB := wbClient(t, server, cookieB)
	docID := "doc-count-port"

	streamA := openWBStream(t, clientA, server, docID, "wbA")
	streamB := openWBStream(t, clientB, server, docID, "wbB")
	defer streamA.close()
	defer streamB.close()
	peersA := countPeersFromEvents(streamA.waitFor(func(ev string) bool {
		return len(countPeersFromEvents([]string{ev})) == 2
	}))
	peersB := countPeersFromEvents(streamB.waitFor(func(ev string) bool {
		return len(countPeersFromEvents([]string{ev})) == 2
	}))
	if len(peersA) != 2 || len(peersB) != 2 {
		t.Fatalf("authoritative count of 2 peers expected, got A=%v B=%v", peersA, peersB)
	}
	if !slices.Contains(peersA, "wbB") || !slices.Contains(peersB, "wbA") {
		t.Fatalf("peer sets disagree: A=%v B=%v", peersA, peersB)
	}
}

// TestWhiteboard_SingleOnlineLabel pins the page regression: exactly
// ONE "online" label on the board page.
func TestWhiteboard_SingleOnlineLabel(t *testing.T) {
	server, _, cookie := wbPortFixture(t)
	client := wbClient(t, server, cookie)
	docID := "doc-online-port"

	resp, err := client.Do(mustGet(t, server.URL+"/whiteboard/"+docID))
	if err != nil {
		t.Fatalf("GET board: %v", err)
	}
	body := readAll(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("board page status = %d", resp.StatusCode)
	}
	if n := strings.Count(body, ">online<"); n != 1 {
		t.Fatalf("expected exactly 1 'online' label, got %d", n)
	}
}

// TestWhiteboard_ThemeToggleWired pins the board page wiring: the
// theme toggle button, its data-on:click signal flip, and both runtimes
// it needs.
func TestWhiteboard_ThemeToggleWired(t *testing.T) {
	server, _, cookie := wbPortFixture(t)
	client := wbClient(t, server, cookie)
	docID := "doc-theme-port"

	resp, err := client.Do(mustGet(t, server.URL+"/whiteboard/"+docID))
	if err != nil {
		t.Fatalf("GET board: %v", err)
	}
	body := readAll(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("board page status = %d", resp.StatusCode)
	}
	wants := []string{
		"theme-toggle", "data-on:click=", "$theme =",
		"/static/theme.js", "/static/datastar.js",
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("board page missing %q (toggle would be inert)", want)
		}
	}
}

// TestWhiteboard_BoardPageRendersValidDocID pins that a board page
// renders for any doc id (the doc is created lazily).
func TestWhiteboard_BoardPageRendersValidDocID(t *testing.T) {
	server, _, cookie := wbPortFixture(t)
	client := wbClient(t, server, cookie)
	docID := "doc-render-port"

	resp, err := client.Do(mustGet(t, server.URL+"/whiteboard/"+docID))
	if err != nil {
		t.Fatalf("GET board: %v", err)
	}
	body := readAll(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("board page status = %d", resp.StatusCode)
	}
	if !strings.Contains(body, docID) {
		t.Fatalf("board page does not render the doc id %q", docID)
	}
}

// TestWhiteboard_NewBoardRedirect pins /whiteboard/new → 302 to a
// concrete doc.
func TestWhiteboard_NewBoardRedirect(t *testing.T) {
	server, _, cookie := wbPortFixture(t)
	client := wbClient(t, server, cookie)

	resp, err := client.Do(mustGet(t, server.URL+"/whiteboard/new"))
	if err != nil {
		t.Fatalf("GET new: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("new board status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, "/whiteboard/") {
		t.Fatalf("new board Location = %q, want /whiteboard/<id>", loc)
	}
}

// TestWhiteboard_BoardPageShowsLoggedInNav pins the authed navbar on the
// board page (the sign-out link only renders for a session).
func TestWhiteboard_BoardPageShowsLoggedInNav(t *testing.T) {
	server, _, cookie := wbPortFixture(t)
	client := wbClient(t, server, cookie)
	docID := "doc-nav-port"

	resp, err := client.Do(mustGet(t, server.URL+"/whiteboard/"+docID))
	if err != nil {
		t.Fatalf("GET board: %v", err)
	}
	body := readAll(t, resp)
	if !strings.Contains(body, "/logout") {
		t.Fatal("board page missing the /logout link (navbar not logged-in-shaped)")
	}
}

// TestWhiteboard_LocalClientDoesNotReceiveEcho pins the no-echo pattern
// explicitly: the originator never gets its own shape back.
func TestWhiteboard_LocalClientDoesNotReceiveEcho(t *testing.T) {
	server, _, cookie := wbPortFixture(t)
	client := wbClient(t, server, cookie)
	docID := "doc-echo-port"

	stream := openWBStream(t, client, server, docID, "wbA")
	defer stream.close()
	stream.settleJoin()

	postOp(t, client, server, docID, "wbA",
		collab.ShapeOp{Op: "add", Shape: collab.Shape{ID: "mine", Type: "rect"}}).Body.Close()

	events := stream.drain(500 * time.Millisecond)
	if shapesEventContains(events, "mine") {
		t.Fatal("originator received its own shape echoed back (no-echo violated)")
	}
}
