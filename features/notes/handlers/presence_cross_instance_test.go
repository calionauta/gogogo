// SCOPE:feature — Cross-instance presence: two servers, one board/note.
//
// These are the tests that make "presence is not per-instance any more" a
// fact rather than a comment. Each one boots TWO independent handlers over two
// independent hubs, joined only by NATS — the shape of two pods behind a load
// balancer — and drives real browser-shaped requests: a tab on instance B opens
// a stream for the doc, a client on instance A joins or leaves, and B's tab
// must see it.
//
// Without the doc bridge every assertion in this file fails with "no
// delivery": BroadcastToDoc/BroadcastExceptToDoc used to be local-only, so a
// peer on another instance was invisible while the presence pill happily read
// "1 online".
package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/nats"
	"github.com/calionauta/gogogo/internal/queue"
)

// noteInstance is one process: its own hub, its own NATS client, its own
// HTTP server. Only the NATS subject space is shared.
type noteInstance struct {
	server *httptest.Server
	cookie *http.Cookie
	hub    *queue.SSEHub
}

// twoNoteInstances boots A and B over one embedded NATS with the doc bridge
// attached on both, and returns them along with a signed-in client per
// instance (two separate accounts, so the roster names differ).
func twoNoteInstances(t *testing.T) (a, b *noteInstance) {
	t.Helper()
	srv, err := nats.StartEmbedded(t.TempDir() + "/nats")
	if err != nil {
		t.Fatalf("embedded nats: %v", err)
	}
	t.Cleanup(srv.Close)

	build := func(tag string) *noteInstance {
		sqlDB, derr := db.OpenAt(t.TempDir() + "/notes-" + tag + ".db")
		if derr != nil {
			t.Fatalf("db open %s: %v", tag, derr)
		}
		t.Cleanup(func() { sqlDB.Close() })
		if merr := dbschema.Migrate(sqlDB); merr != nil {
			t.Fatalf("db migrate %s: %v", tag, merr)
		}
		ah, aerr := auth.New(dbgen.New(sqlDB), false)
		if aerr != nil {
			t.Fatalf("auth new %s: %v", tag, aerr)
		}
		persister, perr := collab.NewTursoPersister(dbgen.New(sqlDB))
		if perr != nil {
			t.Fatalf("persister %s: %v", tag, perr)
		}
		hub := queue.NewSSEHub()
		t.Cleanup(hub.Close)
		conn, cerr := nats.ConnectExisting(srv.URL())
		if cerr != nil {
			t.Fatalf("nats connect %s: %v", tag, cerr)
		}
		t.Cleanup(conn.Close)
		if berr := hub.AttachDocBridge(conn.Conn); berr != nil {
			t.Fatalf("attach doc bridge %s: %v", tag, berr)
		}
		// Presence in production is the federated seam: a local roster plus the
		// peers' published rosters. Both layers are needed — the bridge carries
		// the frames, federation makes the COUNT honest.
		presence := collab.NewFederatedPresence(collab.NewPeerPresence(), hub)
		if aerr := presence.Attach(conn.Conn, "inst-"+tag); aerr != nil {
			t.Fatalf("attach federation %s: %v", tag, aerr)
		}
		federateCtx, stopFederate := context.WithCancel(context.Background())
		t.Cleanup(stopFederate)
		go presence.Run(federateCtx)
		h := NewHandler(dbgen.New(sqlDB), ah, hub, persister, collab.NewDocStore(), config.Load())
		h.SetPresence(presence)
		h.registry = func(context.Context, string) bool { return true }
		mux := http.NewServeMux()
		mux.Handle("/api/auth/", ah.Routes())
		mux.Handle("/", h.Routes())
		server := httptest.NewServer(mux)
		t.Cleanup(server.Close)
		return &noteInstance{
			server: server,
			cookie: notesSignup(t, mux, "presence-"+tag+"@example.com"),
			hub:    hub,
		}
	}
	return build("a"), build("b")
}

// TestNotesPresenceCrossesInstances is the reported gap: two tabs on the same
// note served by different instances. The tab that joined on A must learn
// about the tab that joined on B, and both counts must agree.
//
// RED-PROOFED: remove AttachDocBridge (or the publishDocBridge branch in
// BroadcastToDoc) and B's tab never receives A's join or count.
func TestNotesPresenceCrossesInstances(t *testing.T) {
	a, b := twoNoteInstances(t)
	docID := "note-cross-instance"

	clientA := notesAuthedClient(t, a.server, a.cookie)
	clientB := notesAuthedClient(t, b.server, b.cookie)

	// The tab on B is already watching the note.
	logB, closeB := openNoteStream(t, clientB, b.server, docID, "tab-b")
	defer closeB()
	settleStream(t, logB, "tab-b")

	// A second tab joins on instance A.
	logA, closeA := openNoteStream(t, clientA, a.server, docID, "tab-a")
	defer closeA()
	settleStream(t, logA, "tab-a")

	// The join produced on A reaches the tab on B...
	waitNoteEvent(t, logB, func(msg map[string]any) bool {
		return msg["type"] == "join" && msg["from"] == "tab-a"
	})
	// ...and so does the authoritative recount, which now lists BOTH tabs.
	waitNoteEvent(t, logB, func(msg map[string]any) bool {
		return msg["type"] == "count" && containsAll(peerIDs(msg), "tab-a", "tab-b")
	})
	// A's own tab also sees B, so the two instances agree on the roster.
	waitNoteEvent(t, logA, func(msg map[string]any) bool {
		return msg["type"] == "count" && containsAll(peerIDs(msg), "tab-a", "tab-b")
	})
}

// TestNotesLeaveCrossesInstances pins the departure half: a tab that says
// goodbye on instance A must leave the roster the tab on instance B renders.
//
// RED-PROOFED by removing the connection from the seam (peerLeave/leaveNow
// calling List instead of Leave): B keeps the ghost and the drop times out.
// NOT red when the doc bridge is disabled — Leave() announces a fresh roster,
// so the drop travels on the federation wire. Two healing routes exist, so this
// test measures "the roster converges", not "the bridge works"; the bridge's
// own path is pinned by the cursor tests above.
func TestNotesLeaveCrossesInstances(t *testing.T) {
	a, b := twoNoteInstances(t)
	docID := "note-cross-leave"

	clientA := notesAuthedClient(t, a.server, a.cookie)
	clientB := notesAuthedClient(t, b.server, b.cookie)

	logB, closeB := openNoteStream(t, clientB, b.server, docID, "tab-b")
	defer closeB()
	settleStream(t, logB, "tab-b")

	_, closeA := openNoteStream(t, clientA, a.server, docID, "tab-a")
	defer closeA()

	waitNoteEvent(t, logB, func(msg map[string]any) bool {
		return msg["type"] == "count" && containsAll(peerIDs(msg), "tab-a", "tab-b")
	})

	// The tab on A announces it is going away (the pagehide path).
	if code := postPresence(t, clientA, a.server, docID, "tab-a", "leave", ""); code != http.StatusOK {
		t.Fatalf("leave status = %d, want 200", code)
	}

	waitNoteEvent(t, logB, func(msg map[string]any) bool {
		return msg["type"] == "leave" && msg["from"] == "tab-a"
	})
	// The recount that follows must be B's own roster, without tab-a. The
	// earlier count (both tabs) is in the log too, so match on absence.
	waitNoteEvent(t, logB, func(msg map[string]any) bool {
		if msg["type"] != "count" {
			return false
		}
		peers := peerIDs(msg)
		return containsAll(peers, "tab-b") && !containsAll(peers, "tab-a")
	})
}

// TestNotesCaretCrossesInstances is the fine-grained half: a caret frame is a
// BroadcastExceptToDoc, so it must reach the peer on the other instance and
// still not echo to its own sender.
//
// RED-PROOFED: drop the Except field from the bridge envelope and A's own tab
// receives its caret back; drop the bridge and B never sees it.
func TestNotesCaretCrossesInstances(t *testing.T) {
	a, b := twoNoteInstances(t)
	docID := "note-cross-caret"

	clientA := notesAuthedClient(t, a.server, a.cookie)
	clientB := notesAuthedClient(t, b.server, b.cookie)

	logA, closeA := openNoteStream(t, clientA, a.server, docID, "tab-a")
	defer closeA()
	settleStream(t, logA, "tab-a")

	logB, closeB := openNoteStream(t, clientB, b.server, docID, "tab-b")
	defer closeB()
	settleStream(t, logB, "tab-b")

	code := postPresence(t, clientA, a.server, docID, "tab-a", "caret", `{"line":3,"pos":7}`)
	if code != http.StatusOK {
		t.Fatalf("caret status = %d, want 200", code)
	}

	msg := waitNoteEvent(t, logB, func(msg map[string]any) bool {
		return msg["type"] == "caret" && msg["from"] == "tab-a"
	})
	if x, _ := msg["x"].(float64); x != 3 {
		t.Fatalf("caret line crossed as %v, want 3", msg["x"])
	}
	// No echo back to the sender.
	if echoed, ok := logA.find(func(msg map[string]any) bool {
		return msg["type"] == "caret" && msg["from"] == "tab-a"
	}); ok {
		t.Fatalf("sender's own caret was echoed back: %v", echoed)
	}
}

// TestNotesPresenceStaysOnItsOwnDoc is the negative control: neither the bridge
// nor the federation may turn doc scoping into a global broadcast.
//
// The observable leak is on the OTHER doc's pill: instance B holds a doc-2 tab
// and receives instance A's doc-2 roster, which triggers a doc-2 count. A
// doc-blind merge folds in the doc-1 remote entries B already holds, so B's
// doc-2 tab suddenly lists doc-1's peers. That needs a doc-1 tab on EACH
// instance — with one side empty the doc-blind merge has nothing to fold in.
//
// RED-PROOFED: make FederatedPresence.List merge every doc's remote rosters and
// the doc-2 tab below lists tab-a/tab-b.
func TestNotesPresenceStaysOnItsOwnDoc(t *testing.T) {
	a, b := twoNoteInstances(t)
	clientA := notesAuthedClient(t, a.server, a.cookie)
	clientB := notesAuthedClient(t, b.server, b.cookie)

	// The victim: a doc-2 tab on the SAME instance as doc-1's peer.
	other, closeOther := openNoteStream(t, clientB, b.server, "note-doc-2", "tab-other")
	defer closeOther()

	logB, closeB := openNoteStream(t, clientB, b.server, "note-doc-1", "tab-b")
	defer closeB()

	logA, closeA := openNoteStream(t, clientA, a.server, "note-doc-1", "tab-a")
	defer closeA()

	// A doc-2 tab on the other instance, so B has a remote doc-2 entry to fold.
	_, closeFar := openNoteStream(t, clientA, a.server, "note-doc-2", "tab-far")
	defer closeFar()

	waitNoteEvent(t, logB, func(msg map[string]any) bool {
		return msg["type"] == "count" && containsAll(peerIDs(msg), "tab-a", "tab-b")
	})
	_ = logA

	// B's doc-2 tab must never list doc-1's connections. Allow cross-instance
	// rosters to arrive and be re-broadcast, then audit everything it saw.
	time.Sleep(700 * time.Millisecond)
	if leaked, ok := other.find(func(msg map[string]any) bool {
		if msg["type"] != "count" {
			return false
		}
		return containsAll(peerIDs(msg), "tab-a") || containsAll(peerIDs(msg), "tab-b")
	}); ok {
		t.Fatalf("doc-1 roster leaked into the doc-2 tab: %v", leaked)
	}
}
