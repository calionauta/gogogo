package whiteboard_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/collab"
)

// draftShape returns the first "draft" presence event stamped with user,
// or nil when none arrived. The caller asserts on the shape's id so the
// expected literal lives in the test rather than the helper.
func draftShape(events []string, user string) *collab.Shape {
	for _, ev := range events {
		raw := strings.TrimPrefix(strings.TrimSpace(ev), "data: ")
		var msg collab.PresenceMsg
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			continue
		}
		if msg.Type == "draft" && msg.User == user && msg.Shape != nil {
			return msg.Shape
		}
	}
	return nil
}

// draftMsg returns the first "draft" presence event stamped with user.
// The caller asserts on wire-passthrough fields (cts, cursor) so the
// expected literals live in the test rather than the helper.
func draftMsg(events []string, user string) *collab.PresenceMsg {
	for _, ev := range events {
		raw := strings.TrimPrefix(strings.TrimSpace(ev), "data: ")
		var msg collab.PresenceMsg
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			continue
		}
		if msg.Type == "draft" && msg.User == user && msg.Shape != nil {
			m := msg
			return &m
		}
	}
	return nil
}

// TestWhiteboard_DraftPresenceRelay pins the LIVE-DRAWING channel: an
// in-progress ("draft") shape posted on /presence reaches peers with the
// shape intact and the identity re-stamped, and is NOT echoed to the
// originator. This is what lets a peer watch a stroke grow instead of seeing
// it appear whole on release.
//
// The draft must never be committed: it is relayed on the volatile presence
// path only, so nothing here touches the CRDT or the persisted snapshot.
// Dropping the Shape field from PresenceMsg would relay an empty shell and
// peers would render nothing — this test fails loudly in that case.
func TestWhiteboard_DraftPresenceRelay(t *testing.T) {
	t.Parallel()
	baseURL, _, cleanup := webFixture(t)
	defer cleanup()

	jarA, errA := cookiejar.New(nil)
	if errA != nil {
		t.Fatalf("cookiejar A: %v", errA)
	}
	jarB, errB := cookiejar.New(nil)
	if errB != nil {
		t.Fatalf("cookiejar B: %v", errB)
	}
	clientA := &http.Client{
		Jar: jarA, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	clientB := &http.Client{
		Jar: jarB, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	login(t, clientA, baseURL)
	login(t, clientB, baseURL)

	docID := "doc-draft-" + time.Now().Format("150405.000")
	streamA := openWBStream(t, clientA, baseURL, docID, "wbA")
	streamB := openWBStream(t, clientB, baseURL, docID, "wbB")
	defer streamA.close()
	defer streamB.close()
	streamA.settleJoin()
	streamB.settleJoin()

	shape := collab.Shape{ID: "s-live-1", Type: "rect", X: 10, Y: 20, W: 30, H: 40, Color: "#1f2937"}
	cts := time.Now().UnixMilli()
	draft := collab.PresenceMsg{
		Type: "draft", Doc: docID, User: "spoofed",
		X: 0.22, Y: 0.18, CTS: cts, Shape: &shape, TS: time.Now().UnixMilli(),
	}
	body, mErr := json.Marshal(draft)
	if mErr != nil {
		t.Fatalf("marshal draft: %v", mErr)
	}
	resp, err := postWithClientID(context.Background(), clientA,
		baseURL+"/api/whiteboard/"+docID+"/presence", "wbA", body)
	if err != nil {
		t.Fatalf("draft POST: %v", err)
	}
	resp.Body.Close()

	bEvents := streamB.waitFor(func(ev string) bool {
		s := draftShape([]string{ev}, wbEmail)
		return s != nil && s.ID == "s-live-1"
	})
	aEvents := streamA.drain(100 * time.Millisecond) // absence check: no short-circuit

	if got := draftShape(bEvents, wbEmail); got == nil || got.ID != "s-live-1" {
		t.Fatalf("PEER (clientB) did not receive the draft shape.\nB events:\n%s", debugEvents(bEvents))
	}
	if draftShape(bEvents, "spoofed") != nil {
		t.Fatalf("PEER (clientB) received the spoofed client-sent user on a draft.\nB events:\n%s", debugEvents(bEvents))
	}
	// The fused frame's cursor and capture timestamp ride the same relay
	// untouched: the peer renders the dot from these, so dropping either
	// reopens the skew (or the retry time-travel) this channel closed.
	if got := draftMsg(bEvents, wbEmail); got == nil || got.CTS != cts || got.X != 0.22 || got.Y != 0.18 {
		t.Fatalf("PEER (clientB) did not receive cursor+cts intact on the draft frame "+
			"(got %+v).\nB events:\n%s", got, debugEvents(bEvents))
	}
	if draftShape(aEvents, wbEmail) != nil {
		t.Fatalf("ORIGINATOR (clientA) received its own draft echo "+
			"(exclude-origin broken).\nA events:\n%s", debugEvents(aEvents))
	}
	// The draft must not have been committed: the doc still has no shapes.
	if got := whiteboardShapes(t, baseURL, clientA, docID); len(got) != 0 {
		t.Fatalf("a draft leaked into committed shapes: %v", got)
	}
}
