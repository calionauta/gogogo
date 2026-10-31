// SCOPE:feature — Cross-instance presence: two servers, one board.
//
// The whiteboard half of the presence contract the notes suite also pins: two
// independent handlers over two independent hubs joined only by NATS — two pods
// behind a load balancer — and a tab on instance B that must see a peer arrive
// on instance A, count it, and lose it on leave.
//
// Both layers are exercised and each is proven necessary by mutation:
//   - the hub's doc bridge (BroadcastToDoc/ExceptToDoc → app.docbridge.<doc>)
//     carries the frames;
//   - collab.FederatedPresence (app.roster.<doc>) makes the COUNT the union of
//     every instance's roster. The bridge alone delivered join/leave but every
//     count still read "1 online" while two browsers shared the board.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
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

// wbInstance is one process serving boards: its own hub, NATS client, DB and
// HTTP server. Only the NATS subject space is shared.
type wbInstance struct {
	server *httptest.Server
	cookie *http.Cookie
}

// twoWBInstances boots A and B over one embedded NATS, each with the doc bridge
// and the federated presence seam attached — the production wiring.
func twoWBInstances(t *testing.T) (a, b *wbInstance) {
	t.Helper()
	srv, err := nats.StartEmbedded(t.TempDir() + "/nats")
	if err != nil {
		t.Fatalf("embedded nats: %v", err)
	}
	t.Cleanup(srv.Close)

	build := func(tag string) *wbInstance {
		sqlDB, derr := db.OpenAt(t.TempDir() + "/wb-" + tag + ".db")
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
		presence := collab.NewFederatedPresence(collab.NewPeerPresence(), hub)
		if ferr := presence.Attach(conn.Conn, "inst-"+tag); ferr != nil {
			t.Fatalf("attach federation %s: %v", tag, ferr)
		}
		fedCtx, stopFed := context.WithCancel(context.Background())
		t.Cleanup(stopFed)
		go presence.Run(fedCtx)

		h := NewHandler(dbgen.New(sqlDB), ah, hub, collab.NewMemoryPersister(),
			collab.NewDocStore(), config.Load())
		h.SetPresence(presence)
		h.registry = func(context.Context, string) bool { return true }
		mux := http.NewServeMux()
		mux.Handle("/api/auth/", ah.Routes())
		mux.Handle("/", h.Routes())
		server := httptest.NewServer(mux)
		t.Cleanup(server.Close)
		return &wbInstance{server: server, cookie: wbSignup(t, mux, "wb-"+tag+"@example.com")}
	}
	return build("a"), build("b")
}

// TestWhiteboard_PresenceCrossesInstances is the reported gap on the board: a
// peer that opens the board on instance A must appear in the pill of a tab
// watching the same board on instance B.
//
// RED-PROOFED: drop the FederatedPresence wrapper and B's pill never lists two
// tabs — each instance only counts its own connection. (Dropping only the doc
// bridge is NOT fatal here: federation alone already makes the COUNT the union;
// the bridge is what carries cursor/leave frames, proven by
// TestWhiteboard_CrossInstanceCursorStaysOnItsDoc.)
func TestWhiteboard_PresenceCrossesInstances(t *testing.T) {
	a, b := twoWBInstances(t)
	docID := "wb-cross-instance"
	clientA := wbClient(t, a.server, a.cookie)
	clientB := wbClient(t, b.server, b.cookie)

	streamB := openWBStream(t, clientB, b.server, docID, "wbB")
	defer streamB.close()

	streamA := openWBStream(t, clientA, a.server, docID, "wbA")
	defer streamA.close()

	// B's pill must list BOTH tabs, even though each instance only owns one.
	if peers := waitForPeersAccum(streamB, "wbA", "wbB"); peers == nil {
		t.Fatalf("B never saw the cross-instance roster")
	}
	// A's pill agrees: the union is symmetric.
	if peers := waitForPeersAccum(streamA, "wbA", "wbB"); peers == nil {
		t.Fatalf("A never saw the union roster")
	}
}

// TestWhiteboard_LeaveCrossesInstances pins the departure: closing the tab on A
// must remove it from the roster B renders, not leave a ghost until the lease.
//
// RED-PROOFED by removing the peer from the seam (peerLeave calling List instead
// of Leave): B then keeps the ghost and the drop below times out. NOT red when
// the doc bridge is disabled — Leave() announces a fresh roster, so the drop
// travels on the federation wire and the bridge's leave frame is only the
// instant path. Every healing route has to die for the assertion to fail, which
// is what this test measures: "the roster converges", not "the bridge works".
func TestWhiteboard_LeaveCrossesInstances(t *testing.T) {
	a, b := twoWBInstances(t)
	docID := "wb-cross-leave"
	clientA := wbClient(t, a.server, a.cookie)
	clientB := wbClient(t, b.server, b.cookie)

	streamB := openWBStream(t, clientB, b.server, docID, "wbB")
	defer streamB.close()

	streamA := openWBStream(t, clientA, a.server, docID, "wbA")
	if peers := waitForPeersAccum(streamB, "wbA", "wbB"); peers == nil {
		t.Fatalf("precondition: B never saw the union roster")
	}

	streamA.close()

	// The roster B renders must converge on wbB alone. Assert on a LATER count
	// frame (one that no longer contains wbA), not on the presence of a "leave"
	// frame: the roster is the contract the pill renders.
	deadline := time.Now().Add(wbWaitBudget)
	for time.Now().Before(deadline) {
		for _, ev := range streamB.drain(10 * time.Millisecond) {
			peers := peersOf(ev)
			if peers == nil {
				continue
			}
			if len(peers) == 1 && peers[0] == "wbB" {
				return
			}
		}
	}
	t.Fatalf("B's roster never dropped wbA after the leave: %s", tailEvents(streamB.drain(time.Second)))
}

// TestWhiteboard_PresenceStaysOnItsOwnDoc is the negative control: neither the
// bridge nor the federation may turn doc scoping into a global roster.
//
// The observable leak is on the OTHER doc's pill: instance B holds a doc-2 tab
// and receives instance A's doc-2 roster, which triggers a doc-2 count. A
// doc-blind List() folds in the doc-1 remote entries B already merged, so B's
// doc-2 tab suddenly lists doc-1's peers. Testing this needs a doc-1 tab on
// EACH instance — with only one side populated, the doc-blind merge has nothing
// to fold in and the bug hides.
//
// RED-PROOFED: make FederatedPresence.List merge every doc's remote rosters and
// the doc-2 pill below lists wbA/wbB.
func TestWhiteboard_PresenceStaysOnItsOwnDoc(t *testing.T) {
	a, b := twoWBInstances(t)
	clientA := wbClient(t, a.server, a.cookie)
	clientB := wbClient(t, b.server, b.cookie)

	// A tab on instance B watching a DIFFERENT board — the potential victim.
	nearDoc := openWBStream(t, clientB, b.server, "wb-doc-2", "wbNear")
	defer nearDoc.close()

	current := openWBStream(t, clientB, b.server, "wb-doc-1", "wbB")
	defer current.close()

	streamA := openWBStream(t, clientA, a.server, "wb-doc-1", "wbA")
	defer streamA.close()

	// The same board on instance A, so B has a remote doc-2 entry to fold in.
	farDoc := openWBStream(t, clientA, a.server, "wb-doc-2", "wbFar")
	defer farDoc.close()

	if peers := waitForPeersAccum(current, "wbA", "wbB"); peers == nil {
		t.Fatalf("precondition: doc-1 roster never formed")
	}

	// B's doc-2 pill must list only doc-2 connections. Give the cross-instance
	// rosters time to arrive and be re-broadcast.
	all := nearDoc.drain(700 * time.Millisecond)
	for _, ev := range all {
		peers := peersOf(ev)
		if peers == nil {
			continue
		}
		for _, leaked := range []string{"wbA", "wbB"} {
			if slices.Contains(peers, leaked) {
				t.Fatalf("doc-1 peer %q leaked into the doc-2 roster: %v", leaked, peers)
			}
		}
	}
}

// TestWhiteboard_CrossInstanceCursorStaysOnItsDoc pins the fine-grained frame:
// a cursor posted on instance A reaches the peer on B for the SAME board only.
//
// RED-PROOFED: drop the doc argument from the bridge (one global subject) and
// the doc-2 tab on B receives a cursor meant for doc-1.
func TestWhiteboard_CrossInstanceCursorStaysOnItsDoc(t *testing.T) {
	a, b := twoWBInstances(t)
	clientA := wbClient(t, a.server, a.cookie)
	clientB := wbClient(t, b.server, b.cookie)

	sameDoc := openWBStream(t, clientB, b.server, "wb-cursor-1", "wbB1")
	defer sameDoc.close()

	otherDoc := openWBStream(t, clientB, b.server, "wb-cursor-2", "wbB2")
	defer otherDoc.close()

	body, _ := json.Marshal(map[string]any{"type": "cursor", "x": 10, "y": 20})
	resp, err := postWithClientID(context.Background(), clientA,
		a.server.URL+"/api/whiteboard/wb-cursor-1/presence", "wbA", body)
	if err != nil {
		t.Fatalf("post cursor: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cursor status = %d", resp.StatusCode)
	}

	events := sameDoc.waitFor(func(ev string) bool {
		return containsCursor(ev, "wb-cursor-1")
	})
	if !slices.ContainsFunc(events, func(ev string) bool { return containsCursor(ev, "wb-cursor-1") }) {
		t.Fatalf("peer on the other instance never saw the cursor; events: %s", tailEvents(events))
	}
	for _, ev := range otherDoc.drain(300 * time.Millisecond) {
		if containsCursor(ev, "wb-cursor-1") {
			t.Fatalf("doc-1 cursor leaked into the doc-2 tab: %s", ev)
		}
	}
}

// waitForPeersAccum waits until a count frame lists every wanted client id,
// accumulating EVERY frame seen so far. The roster helper in port_test.go
// inspects one drain window at a time, which is right when the caller controls
// what has already been consumed (settleJoin) but loses the union frame here:
// the frame proving the cross-instance roster can arrive before the local one.
func waitForPeersAccum(s *wbStream, want ...string) []string {
	deadline := time.Now().Add(wbWaitBudget)
	var seen []string
	for time.Now().Before(deadline) {
		seen = append(seen, s.drain(10*time.Millisecond)...)
		if peers := firstPeersWith(seen, want...); peers != nil {
			return peers
		}
	}
	return nil
}

// containsCursor reports whether ev is a cursor frame tagged with docID.
func containsCursor(ev, docID string) bool {
	var msg collab.PresenceMsg
	// The frame is "data: <json>"; TrimPrefix is the same unwrap peersOf uses.
	if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(ev), "data: ")), &msg); err != nil {
		return false
	}
	return msg.Type == "cursor" && msg.Doc == docID
}
