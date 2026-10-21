// SCOPE:feature — Shared-board contract for the whiteboard (author edits,
// author or admin deletes).
//
// The demo pair is the spec: demo1 is admin, demo2 is viewer. Both must
// OPEN and DRAW on any board (sharing is what proves live collab from two
// browsers), and only the author — or an admin — may DELETE it. These
// tests drive the real HTTP slice over a real Turso file with two real
// sessions, so the contract is proven end to end rather than inferred
// from a helper.
package handlers

import (
	"context"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/queue"
)

// TestCanDeleteBoard pins the rule table in one place. Red-proof: return
// true for a plain peer, or drop the admin arm, and it fails.
func TestCanDeleteBoard(t *testing.T) {
	for _, tc := range []struct {
		name              string
		role              string
		requester, author string
		want              bool
	}{
		{"author viewer", "viewer", "u1", "u1", true},
		{"author user", "user", "u1", "u1", true},
		{"author admin", "admin", "u1", "u1", true},
		{"peer viewer", "viewer", "u2", "u1", false},
		{"peer user", "user", "u2", "u1", false},
		{"admin foreign", "admin", "u9", "u1", true},
		{"anonymous", "", "", "u1", false},
	} {
		if got := canDeleteDoc(tc.role, tc.requester, tc.author); got != tc.want {
			t.Errorf("%s: canDeleteDoc(%q,%q,%q) = %v, want %v",
				tc.name, tc.role, tc.requester, tc.author, got, tc.want)
		}
	}
}

// wbSharedFixture boots the whiteboard slice and returns two sessions plus
// the query handle, so a test can assert rows (did the delete really land?)
// and flip a role the way boot promotion does. The middleware resolves the
// role per request, so a role update is live without a re-login.
func wbSharedFixture(t *testing.T) (server *httptest.Server, q *dbgen.Queries, author, peer *http.Cookie) {
	t.Helper()
	server, q, _, author, peer = wbSharedFixtureWithStore(t, false)
	return server, q, author, peer
}

// wbSharedFixtureWithStore is wbSharedFixture with the persister handed
// out, for the tests that assert the durable half (snapshot gone after a
// delete) rather than the permissions. useStore=false gives a throwaway
// store; the fixture returns the one the handler actually uses so the two
// are never confused.
func wbSharedFixtureWithStore(t *testing.T, wantStore bool) (
	server *httptest.Server, q *dbgen.Queries, mem *collab.MemoryPersister, author, peer *http.Cookie,
) {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/wb-shared.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("db migrate: %v", merr)
	}
	q = dbgen.New(sqlDB)
	a, aerr := auth.New(q, false)
	if aerr != nil {
		t.Fatalf("auth new: %v", aerr)
	}
	mem = collab.NewMemoryPersister()
	_ = wantStore
	h := NewHandler(q, a, queue.NewSSEHub(), mem, collab.NewDocStore(), config.Load())
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Routes())
	mux.Handle("/", h.Routes())
	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, q, mem, wbSignup(t, mux, wbEmail), wbSignup(t, mux, "peer@example.com")
}

// createBoardAs creates a board through the real /whiteboard/new handler
// and returns its doc id.
func createBoardAs(t *testing.T, server *httptest.Server, cookie *http.Cookie) string {
	t.Helper()
	client := wbClient(t, server, cookie)
	resp, err := client.Do(mustGet(t, server.URL+"/whiteboard/new"))
	if err != nil {
		t.Fatalf("new board: %v", err)
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/whiteboard/") {
		t.Fatalf("new board Location = %q", loc)
	}
	return strings.TrimPrefix(loc, "/whiteboard/")
}

// TestWhiteboardIndexIsSharedAndNamesTheAuthor pins the listing change:
// another login's index must show the board (it used to be empty), name
// the author, and only offer a delete control where the server granted the
// right.
func TestWhiteboardIndexIsSharedAndNamesTheAuthor(t *testing.T) {
	server, _, authorCookie, peerCookie := wbSharedFixture(t)
	docID := createBoardAs(t, server, authorCookie)

	peer := wbClient(t, server, peerCookie)
	body := wbRead(t, peer, server.URL+"/api/whiteboards/fragment")
	if !strings.Contains(body, docID) {
		t.Fatalf("shared index must list the author's board %q; body=%s", docID, body)
	}
	if !strings.Contains(body, "by "+wbEmail) {
		t.Errorf("shared index must name the author; body=%s", body)
	}
	if strings.Contains(body, "confirm-delete-board") {
		t.Errorf("peer must not get a delete control for a foreign board; body=%s", body)
	}
}

// TestWhiteboardAuthorControlsAreStamped pins the same list for the author:
// the row is theirs ("you") and carries the delete control.
func TestWhiteboardAuthorControlsAreStamped(t *testing.T) {
	server, _, authorCookie, _ := wbSharedFixture(t)
	docID := createBoardAs(t, server, authorCookie)

	author := wbClient(t, server, authorCookie)
	body := wbRead(t, author, server.URL+"/api/whiteboards/fragment")
	if !strings.Contains(body, docID) {
		t.Fatalf("author's own list is missing %q", docID)
	}
	if !strings.Contains(body, ">you<") {
		t.Errorf("author row must render the 'you' badge; body=%s", body)
	}
	if !strings.Contains(body, "confirm-delete-board") {
		t.Errorf("author row must wire the delete confirmation; body=%s", body)
	}
	page := wbRead(t, author, server.URL+"/whiteboard")
	if !strings.Contains(page, "/api/whiteboard/' + encodeURIComponent($confirmBoardId) + '/delete") {
		t.Errorf("index must wire the delete POST to the selected board; body=%s", page)
	}
}

// TestWhiteboardPeerCanOpenAndDraw pins the edit half of the contract: a
// second login opens someone else's board and its shape op lands.
func TestWhiteboardPeerCanOpenAndDraw(t *testing.T) {
	server, _, authorCookie, peerCookie := wbSharedFixture(t)
	docID := createBoardAs(t, server, authorCookie)

	peer := wbClient(t, server, peerCookie)
	page := wbRead(t, peer, server.URL+"/whiteboard/"+docID)
	if !strings.Contains(page, docID) {
		t.Fatalf("peer cannot open the shared board page")
	}
	resp, err := postWithClientID(context.Background(), peer,
		server.URL+"/api/whiteboard/"+docID+"/update", "peerTab",
		[]byte(`{"op":"add","shape":{"id":"peer-shape","type":"rect","x":5}}`))
	if err != nil {
		t.Fatalf("peer draw: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("peer draw status = %d, want 200", resp.StatusCode)
	}
	if body := wbRead(t, peer, server.URL+"/api/whiteboard/"+docID+"/snapshot"); !strings.Contains(body, "peer-shape") {
		t.Fatalf("peer's shape did not land on the shared board; snapshot=%s", body)
	}
}

// TestWhiteboardDeletePermission pins the delete half: a plain peer is
// refused (403) and the board survives, a viewer on a foreign board is
// refused too, and an admin deletes any board.
func TestWhiteboardDeletePermission(t *testing.T) {
	server, q, authorCookie, peerCookie := wbSharedFixture(t)
	ctx := context.Background()

	// Author deletes their own board.
	authored := createBoardAs(t, server, authorCookie)
	author := wbClient(t, server, authorCookie)
	if code := wbDelete(t, author, server, authored); code != http.StatusOK {
		t.Fatalf("author delete status = %d, want 200", code)
	}
	if _, err := q.GetBoardMeta(ctx, authored); err == nil {
		t.Fatal("author delete left the metadata row behind")
	}

	// A plain peer cannot delete what the other user started.
	foreign := createBoardAs(t, server, authorCookie)
	peer := wbClient(t, server, peerCookie)
	if code := wbDelete(t, peer, server, foreign); code != http.StatusForbidden {
		t.Fatalf("peer delete status = %d, want 403", code)
	}
	if _, err := q.GetBoardMeta(ctx, foreign); err != nil {
		t.Fatalf("refused delete must leave the board in place: %v", err)
	}

	// A viewer on a foreign board is refused the same way.
	if err := q.SetUserRole(ctx, dbgen.SetUserRoleParams{
		Role: "viewer", Updated: dbschema.Now(), Email: "peer@example.com",
	}); err != nil {
		t.Fatalf("set viewer role: %v", err)
	}
	if code := wbDelete(t, peer, server, foreign); code != http.StatusForbidden {
		t.Fatalf("viewer delete of a foreign board = %d, want 403", code)
	}

	// An admin deletes any board.
	adminCookie := wbSignup(t, server.Config.Handler, "boss@example.com")
	if err := q.SetUserRole(ctx, dbgen.SetUserRoleParams{
		Role: "admin", Updated: dbschema.Now(), Email: "boss@example.com",
	}); err != nil {
		t.Fatalf("set admin role: %v", err)
	}
	admin := wbClient(t, server, adminCookie)
	if code := wbDelete(t, admin, server, foreign); code != http.StatusOK {
		t.Fatalf("admin delete of a foreign board = %d, want 200", code)
	}
	if _, err := q.GetBoardMeta(ctx, foreign); err == nil {
		t.Fatal("admin delete left the metadata row behind")
	}
}

// TestWhiteboardDeleteDropsSnapshotAndCachedDoc pins the durable half:
// after a delete the snapshot is gone and re-opening the id starts empty
// instead of resurrecting the drawing.
func TestWhiteboardDeleteDropsSnapshotAndCachedDoc(t *testing.T) {
	server, _, mem, authorCookie, _ := wbSharedFixtureWithStore(t, true)
	docID := createBoardAs(t, server, authorCookie)
	client := wbClient(t, server, authorCookie)

	seed := collab.ShapeOp{Op: "add", Shape: collab.Shape{ID: "gone-shape", Type: "rect", X: 1}}
	if resp := postOp(t, client, server, docID, "wbA", seed); resp.StatusCode != http.StatusOK {
		t.Fatalf("seed draw status = %d", resp.StatusCode)
	}
	if snap, ok := mem.LoadSnapshot(docID); !ok || len(snap) == 0 {
		t.Fatal("seed draw did not persist a snapshot")
	}
	if code := wbDelete(t, client, server, docID); code != http.StatusOK {
		t.Fatalf("delete status = %d", code)
	}
	if _, ok := mem.LoadSnapshot(docID); ok {
		t.Fatal("delete left the snapshot behind — the drawing is still durable")
	}
	// Re-opening the deleted board must not resurrect the shape.
	snapshot := wbRead(t, client, server.URL+"/api/whiteboard/"+docID+"/snapshot")
	if strings.Contains(snapshot, "gone-shape") {
		t.Fatalf("deleted board rehydrated its shapes: %s", snapshot)
	}
}

func wbRead(t *testing.T, client *http.Client, url string) string {
	t.Helper()
	resp, err := client.Do(mustGet(t, url))
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	raw, rerr := io.ReadAll(resp.Body)
	if rerr != nil {
		t.Fatalf("read body: %v", rerr)
	}
	return html.UnescapeString(string(raw))
}

func wbDelete(t *testing.T, client *http.Client, server *httptest.Server, docID string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		server.URL+"/api/whiteboard/"+neturl.PathEscape(docID)+"/delete", nil)
	if err != nil {
		t.Fatalf("delete req: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("delete post: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}
