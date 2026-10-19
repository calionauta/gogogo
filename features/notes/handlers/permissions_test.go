// SCOPE:feature — Shared-doc contract for notes (author edits, author or
// admin deletes).
//
// The demo pair is the spec: demo1 is admin, demo2 is viewer. Both must
// OPEN and EDIT any note (sharing is what proves live collab from two
// browsers), and only the author — or an admin — may DELETE it. These
// tests drive the real HTTP slice over a real Turso file, with two real
// sessions, so the contract is proven end to end rather than inferred
// from a helper.
package handlers

import (
	"context"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// TestCanDeleteDoc pins the rule table in one place. Red-proof: return
// true for a plain peer, or drop the admin arm, and it fails.
func TestCanDeleteDoc(t *testing.T) {
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

// notesSharedFixture boots the notes slice and returns two sessions plus
// the query handle, so a test can assert rows (did the delete really land?)
// and flip a role the way boot promotion does. The middleware resolves the
// role per request, so a role update is live without a re-login.
//
// It is a local copy of notesPortFixture with the handle exposed: the port
// fixture is shared by a dozen tests and widening its signature would
// churn them all for one caller.
func notesSharedFixture(t *testing.T) (server *httptest.Server, q *dbgen.Queries, author, peer *http.Cookie) {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/notes-shared.db")
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
	h := NewHandler(q, a, queue.NewSSEHub(), collab.NewMemoryPersister(),
		collab.NewDocStore(), config.Load())
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Handler())
	mux.Handle("/", h.Routes())
	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, q, notesSignup(t, mux, notesEmail), notesSignup(t, mux, "peer@example.com")
}

// createNoteAs creates a note through the real /notes/new handler and
// returns its doc id.
func createNoteAs(t *testing.T, server *httptest.Server, cookie *http.Cookie) string {
	t.Helper()
	client := notesAuthedClient(t, server, cookie)
	return createNoteForTest(t, client, server)
}

// TestNotesIndexIsSharedAndNamesTheAuthor pins the listing change: another
// login's index must show the note (it used to be empty), name the author,
// and only offer a delete control where the server granted the right.
func TestNotesIndexIsSharedAndNamesTheAuthor(t *testing.T) {
	server, _, authorCookie, peerCookie := notesSharedFixture(t)
	docID := createNoteAs(t, server, authorCookie)

	peerClient := notesAuthedClient(t, server, peerCookie)
	resp := doGet(t, peerClient, server.URL+"/api/notes/fragment")
	body := readBody(t, resp)
	if !strings.Contains(body, docID) {
		t.Fatalf("shared index must list the author's note %q; body=%s", docID, body)
	}
	if !strings.Contains(body, "by notes@example.com") {
		t.Errorf("shared index must name the author; body=%s", body)
	}
	// The peer is a plain user (neither author nor admin): edit yes,
	// delete no — so no delete control may render on that row.
	if strings.Contains(body, "/delete'") {
		t.Errorf("peer must not get a delete control for a foreign note; body=%s", body)
	}
}

// TestNotesAuthorControlsAreStamped pins the same list for the author: the
// row is theirs ("you") and carries the delete control.
func TestNotesAuthorControlsAreStamped(t *testing.T) {
	server, _, authorCookie, _ := notesSharedFixture(t)
	docID := createNoteAs(t, server, authorCookie)

	authorClient := notesAuthedClient(t, server, authorCookie)
	body := readBody(t, doGet(t, authorClient, server.URL+"/api/notes/fragment"))
	if !strings.Contains(body, docID) {
		t.Fatalf("author's own list is missing %q", docID)
	}
	if !strings.Contains(body, ">you<") {
		t.Errorf("author row must render the 'you' badge; body=%s", body)
	}
	// The row carries the modal opener (the confirm dialog itself lives on
	// the index page, once, shared by every row).
	if !strings.Contains(body, "confirm-delete-note") {
		t.Errorf("author row must wire the delete confirmation; body=%s", body)
	}
	// And the page really posts the delete route, bound to the signal.
	page := readBody(t, doGet(t, authorClient, server.URL+"/notes"))
	if !strings.Contains(page, "/api/notes/' + encodeURIComponent($confirmNoteId) + '/delete") {
		t.Errorf("index must wire the delete POST to the selected note; body=%s", page)
	}
}

// TestNotesPeerCanOpenAndEdit pins the edit half of the contract: a second
// login opens someone else's note and posts an op successfully. Without
// this the "shared" claim is only a listing change.
func TestNotesPeerCanOpenAndEdit(t *testing.T) {
	server, _, authorCookie, peerCookie := notesSharedFixture(t)
	docID := createNoteAs(t, server, authorCookie)

	peerClient := notesAuthedClient(t, server, peerCookie)
	page := readBody(t, doGet(t, peerClient, server.URL+"/notes/"+docID))
	if !strings.Contains(page, docID) {
		t.Fatalf("peer cannot open the shared note page; status body=%s", page)
	}
	code, out := postOp(t, peerClient, server, docID, "peerTab",
		`{"base":0,"ops":[{"t":"ins","i":0,"s":"edited by peer"}]}`)
	if code != http.StatusOK {
		t.Fatalf("peer edit status = %d, want 200 (%v)", code, out)
	}
	if out["text"] != "edited by peer" {
		t.Fatalf("peer edit text = %v", out["text"])
	}
}

// TestNotesDeletePermission pins the delete half: a plain peer is refused
// (403) and the note survives, an admin deletes a foreign note, and the
// author deletes their own.
func TestNotesDeletePermission(t *testing.T) {
	server, q, authorCookie, peerCookie := notesSharedFixture(t)
	ctx := context.Background()

	// Author deletes their own note.
	authored := createNoteAs(t, server, authorCookie)
	authorClient := notesAuthedClient(t, server, authorCookie)
	if code := postDelete(t, authorClient, server, authored); code != http.StatusOK {
		t.Fatalf("author delete status = %d, want 200", code)
	}
	if _, err := q.GetNoteMeta(ctx, authored); err == nil {
		t.Fatal("author delete left the metadata row behind")
	}

	// A plain peer cannot delete what the other user started.
	foreign := createNoteAs(t, server, authorCookie)
	peerClient := notesAuthedClient(t, server, peerCookie)
	if code := postDelete(t, peerClient, server, foreign); code != http.StatusForbidden {
		t.Fatalf("peer delete status = %d, want 403", code)
	}
	if _, err := q.GetNoteMeta(ctx, foreign); err != nil {
		t.Fatalf("refused delete must leave the note in place: %v", err)
	}

	// A viewer-delete of a foreign note is refused the same way.
	if err := q.SetUserRole(ctx, dbgen.SetUserRoleParams{
		Role: "viewer", Updated: dbschema.Now(), Email: "peer@example.com",
	}); err != nil {
		t.Fatalf("set viewer role: %v", err)
	}
	if code := postDelete(t, peerClient, server, foreign); code != http.StatusForbidden {
		t.Fatalf("viewer delete of a foreign note = %d, want 403", code)
	}

	// An admin deletes any note, and the board's snapshot goes with it.
	adminCookie := notesSignup(t, server.Config.Handler, "boss@example.com")
	if err := q.SetUserRole(ctx, dbgen.SetUserRoleParams{
		Role: "admin", Updated: dbschema.Now(), Email: "boss@example.com",
	}); err != nil {
		t.Fatalf("set admin role: %v", err)
	}
	adminClient := notesAuthedClient(t, server, adminCookie)
	if code := postDelete(t, adminClient, server, foreign); code != http.StatusOK {
		t.Fatalf("admin delete of a foreign note = %d, want 200", code)
	}
	if _, err := q.GetNoteMeta(ctx, foreign); err == nil {
		t.Fatal("admin delete left the metadata row behind")
	}
}

// TestNotesDeleteDropsSnapshotAndCachedDoc pins the durable half: after a
// delete, the snapshot is gone and a re-created id starts empty rather
// than resurrecting the deleted text.
func TestNotesDeleteDropsSnapshotAndCachedDoc(t *testing.T) {
	server, persister, cookie := notesPortFixture(t)
	client := notesAuthedClient(t, server, cookie)
	docID := createNoteForTest(t, client, server)

	if code, _ := postOp(t, client, server, docID, "cliA",
		`{"base":0,"ops":[{"t":"ins","i":0,"s":"secret text"}]}`); code != http.StatusOK {
		t.Fatalf("seed op status = %d", code)
	}
	if snap, ok := persister.LoadSnapshot(docID); !ok || len(snap) == 0 {
		t.Fatal("seed op did not persist a snapshot")
	}
	if code := postDelete(t, client, server, docID); code != http.StatusOK {
		t.Fatalf("delete status = %d", code)
	}
	if _, ok := persister.LoadSnapshot(docID); ok {
		t.Fatal("delete left the snapshot behind — the text is still durable")
	}
	// Opening the deleted doc again must not rehydrate the old text.
	page := readBody(t, doGet(t, client, server.URL+"/notes/"+docID))
	if strings.Contains(page, "secret text") {
		t.Fatal("deleted note rehydrated its old text")
	}
}

// readBody drains a response and fails the test on a read error.
func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return html.UnescapeString(string(raw))
}

// postDelete fires the delete route for docID.
func postDelete(t *testing.T, client *http.Client, server *httptest.Server, docID string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		server.URL+"/api/notes/"+url.PathEscape(docID)+"/delete", nil)
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
