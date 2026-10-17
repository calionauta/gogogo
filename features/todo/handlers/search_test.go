// SCOPE:feature — the todo search slice's contract: the endpoint narrows an
// already-scoped list, ranking survives, and the index write is atomic with
// the row it describes.
package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/features/todo"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/search"
)

// TestSearchSubstringIsTheDefaultLayer pins the default mode end to end: with
// SEARCH_MODE unset (substring) the endpoint still narrows, and it does so
// with NO index — no postings row is written, which is the whole point of the
// default. A middle substring must match (an exact-match-only filter would
// return nothing here).
//
// Red-proof: point the default at BM25 and the posting-count assertion fails;
// make the layer exact-match and the middle-substring assertion fails.
func TestSearchSubstringIsTheDefaultLayer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newHandlersFixture(t)
	layer, err := search.New(search.SubstringMode, NewTodoSource(f.q), nil)
	if err != nil {
		t.Fatalf("search.New: %v", err)
	}
	f.h.SetSearch(NewTodoSearcher(layer), nil, nil)
	cookie := f.signup("substring@example.com")
	me := f.ownerOf(t, cookie)
	createStamped(t, f, me, "deploy the parser diff")
	createStamped(t, f, me, "unrelated row")

	body := searchJSON(t, f, cookie, "parser").Body.String()
	if !contains(body, "deploy the parser diff") {
		t.Errorf("middle substring did not match:\n%s", body)
	}
	if contains(body, "unrelated row") {
		t.Error("non-matching row survived the substring filter")
	}
	// The default writes no index: nothing to keep in step on a mutation.
	var postings int
	if err := f.sqlDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM search_postings WHERE owner = ?`, me).Scan(&postings); err != nil {
		t.Fatalf("count postings: %v", err)
	}
	if postings != 0 {
		t.Errorf("substring mode wrote %d postings, want 0 (the default must cost no index)", postings)
	}
}

// wireBM25 gives a fixture the indexed mode: Source + postings index + pool,
// so both the read ranking and the write path are exercised.
func wireBM25(t *testing.T, f *handlersFixture) {
	t.Helper()
	ix := search.SQLIndex{DB: f.sqlDB}
	layer, err := search.New(search.BM25Mode, NewTodoSource(f.q), ix)
	if err != nil {
		t.Fatalf("search.New: %v", err)
	}
	f.h.SetSearch(NewTodoSearcher(layer), NewTodoIndexer(ix), f.sqlDB)
}

// createStamped inserts a todo with the timestamps the list path parses
// (the shared `create` helper in search_source_test.go omits them, which is
// fine for the query-level tests there but 500s a full list render).
func createStamped(t *testing.T, f *handlersFixture, owner, title string) {
	t.Helper()
	now := dbschema.Now()
	if _, err := f.q.CreateTodo(context.Background(), dbgen.CreateTodoParams{
		ID: dbschema.NewID(), Owner: owner, Title: title,
		Visibility: todo.VisibilityPrivate, Created: now, Updated: now,
	}); err != nil {
		t.Fatalf("create %q: %v", title, err)
	}
}

// searchJSON drives GET /api/todos/search and returns the recorder. The
// handler answers SSE, so assertions read the patched region rather than a
// JSON body.
func searchJSON(t *testing.T, f *handlersFixture, cookie *http.Cookie, query string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/api/todos/search?q=" + url.QueryEscape(query)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d (body: %s)", target, rec.Code, rec.Body.String())
	}
	return rec
}

// TestSearchEndpointNarrowsScopedList is the feature's core claim: search is
// a NARROWING of the caller's visible set, never a widening. The endpoint
// must exclude another owner's private row even though its title matches, and
// must drop a stale posting whose row is gone rather than resurrect it.
//
// Red-proof: make narrowBySearch return the raw ranked ids instead of
// intersecting with the visible rows, and the private-row assertion fails
// (a forged posting can then surface a title the caller may not list).
func TestSearchEndpointNarrowsScopedList(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newHandlersFixture(t)
	wireBM25(t, f)
	myCookie := f.signup("searcher-a@example.com")
	mine := f.ownerOf(t, myCookie)
	theirs := f.ownerOf(t, f.signup("searcher-b@example.com"))

	// My row is indexed through the same atomic path the handler uses.
	myID := dbschema.NewID()
	if err := createTodoIndexed(ctx, f.q, f.h.Index, f.h.DB, dbgen.CreateTodoParams{
		ID: myID, Owner: mine, Title: "smoke the parser",
		Visibility: todo.VisibilityPrivate, Created: dbschema.Now(), Updated: dbschema.Now(),
	}); err != nil {
		t.Fatalf("indexed create: %v", err)
	}
	// A matching row owned by someone else, indexed too: search must not
	// surface it, because the caller cannot list it.
	if err := createTodoIndexed(ctx, f.q, f.h.Index, f.h.DB, dbgen.CreateTodoParams{
		ID: dbschema.NewID(), Owner: theirs, Title: "smoke signals elsewhere",
		Visibility: todo.VisibilityPrivate, Created: dbschema.Now(), Updated: dbschema.Now(),
	}); err != nil {
		t.Fatalf("indexed create (other): %v", err)
	}
	// A posting with NO row: the shape a crash between index and row (or a
	// hand-edited index) leaves behind. The visible set has no such id, so
	// navigation by rank must skip it.
	if _, err := f.sqlDB.ExecContext(ctx,
		`INSERT INTO search_postings (owner, kind, doc_id, field, term, count, length)
		 VALUES (?, 'todo', 'ghost-id', 'title', 'smoke', 1, 2)`, mine); err != nil {
		t.Fatalf("plant ghost posting: %v", err)
	}

	rec := searchJSON(t, f, myCookie, "smoke")
	body := rec.Body.String()
	if !contains(body, "smoke the parser") {
		t.Errorf("my matching row missing from the patched list:\n%s", body)
	}
	if contains(body, "smoke signals elsewhere") {
		t.Error("another owner's matching row leaked into my search results")
	}
	if contains(body, "ghost-id") {
		t.Error("a posting without a row was rendered (should be skipped)")
	}
}

// TestSearchRanksByScore pins that the plugin's ordering, not list order,
// drives the result order. BM25 makes a rare term beat a common one, so the
// short document stating "kestrel" once must come before the long one
// repeating it among many other words.
//
// Rows are located by their rendered container id, not by title text: the
// short title is a substring of the long one, so a text search would measure
// the long row in both cases and the assertion would be vacuous.
//
// Red-proof: drop the byID reorder (append in list order instead) and the
// position assertion fails — list order (created DESC) is deliberately the
// REVERSE of the ranking here.
func TestSearchRanksByScore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newHandlersFixture(t)
	wireBM25(t, f)
	cookie := f.signup("ranker@example.com")
	me := f.ownerOf(t, cookie)

	long := "kestrel kestrel alpha bravo charlie delta echo foxtrot golf hotel india juliet"
	short := "kestrel"
	// Created short-first so list order (created DESC) puts the LONG doc
	// first while BM25 must put the SHORT one first: the two orders oppose.
	shortID, longID := dbschema.NewID(), dbschema.NewID()
	for _, row := range []struct{ id, title string }{{shortID, short}, {longID, long}} {
		if err := createTodoIndexed(ctx, f.q, f.h.Index, f.h.DB, dbgen.CreateTodoParams{
			ID: row.id, Owner: me, Title: row.title,
			Visibility: todo.VisibilityPrivate, Created: dbschema.Now(), Updated: dbschema.Now(),
		}); err != nil {
			t.Fatalf("indexed create %q: %v", row.title, err)
		}
	}

	body := searchJSON(t, f, cookie, "kestrel").Body.String()
	shortAt, longAt := indexOf(body, `id="todo-`+shortID), indexOf(body, `id="todo-`+longID)
	if shortAt < 0 || longAt < 0 {
		t.Fatalf("both rows must be patched (short=%d long=%d)", shortAt, longAt)
	}
	if shortAt > longAt {
		t.Errorf("short doc ranked below the long one (short@%d long@%d): BM25 ordering lost", shortAt, longAt)
	}
}

// TestIndexWriteIsAtomicWithRow is the write-path claim: a delete removes the
// row AND its postings in one transaction, so no posting outlives its row.
//
// Red-proof: swap deleteTodoIndexed for a bare DeleteTodoByID in the handler
// and the posting count below stays at 1.
func TestIndexWriteIsAtomicWithRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newHandlersFixture(t)
	wireBM25(t, f)
	me := f.ownerOf(t, f.signup("atomic@example.com"))
	id := dbschema.NewID()
	if err := createTodoIndexed(ctx, f.q, f.h.Index, f.h.DB, dbgen.CreateTodoParams{
		ID: id, Owner: me, Title: "orphan candidate",
		Visibility: todo.VisibilityPrivate, Created: dbschema.Now(), Updated: dbschema.Now(),
	}); err != nil {
		t.Fatalf("indexed create: %v", err)
	}
	if got := postingCount(t, f.sqlDB, me, id); got != 2 {
		t.Fatalf("postings after create = %d, want 2 (orphan, candidate)", got)
	}
	if err := deleteTodoIndexed(ctx, f.q, f.h.Index, f.h.DB, me, id); err != nil {
		t.Fatalf("indexed delete: %v", err)
	}
	if got := postingCount(t, f.sqlDB, me, id); got != 0 {
		t.Errorf("postings after delete = %d, want 0 (index outlived its row)", got)
	}
}

// TestSearchDegradesWhenPluginAbsent pins the removal contract: with no
// Searcher wired (trimmed plugin), the list endpoint still answers with the
// full visible set and the template hides the box. A trimmed build must not
// 500 on a search-narrowed list.
//
// Red-proof: make narrowBySearch error instead of returning the list when
// Search is nil, and the status assertion fails.
func TestSearchDegradesWhenPluginAbsent(t *testing.T) {
	t.Parallel()
	f := newHandlersFixture(t)
	cookie := f.signup("trimmed@example.com")
	me := f.ownerOf(t, cookie)
	createStamped(t, f, me, "unaffected row")

	// f.h.Search is nil: the fixture never wires a searcher.
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/todos", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list with no search wired: status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if !contains(rec.Body.String(), "unaffected row") {
		t.Error("without the plugin the list must show every visible row")
	}
}

// TestMutationKeepsActiveSearchNarrowing pins a bug the browser smoke found:
// a mutation re-lists through patchListSelf, and that re-list must respect the
// tab's ACTIVE search. A mutation URL carries no `q`, so before the fix the
// create/toggle/delete path re-rendered every visible row — silently dropping
// the filter and (because the box binds the same signal) wiping what the user
// had typed.
//
// Red-proof: make patchListSelf ignore ViewOf(...).Query and the every-row
// assertion fails.
func TestMutationKeepsActiveSearchNarrowing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newHandlersFixture(t)
	wireBM25(t, f)
	cookie := f.signup("midsearch@example.com")
	me := f.ownerOf(t, cookie)

	// Two rows: one matches "kestrel", one does not.
	if err := createTodoIndexed(ctx, f.q, f.h.Index, f.h.DB, dbgen.CreateTodoParams{
		ID: dbschema.NewID(), Owner: me, Title: "kestrel report",
		Visibility: todo.VisibilityPrivate, Created: dbschema.Now(), Updated: dbschema.Now(),
	}); err != nil {
		t.Fatalf("indexed create: %v", err)
	}
	createStamped(t, f, me, "unrelated chore")

	// The tab performs a search, which stores its query on the stream view.
	const clientID = "tab-midsearch"
	searchJSONWithClient(t, f, cookie, "kestrel", clientID)

	// Now a mutation from the same tab: it must still narrow to the search.
	form := url.Values{"title": {"kestrel follow-up"}}
	req := httptest.NewRequestWithContext(ctx, http.MethodPost,
		"/api/todos?filter=all&scope=all&clientID="+clientID, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create during search: status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !contains(body, "kestrel report") {
		t.Errorf("matching row missing after mutation:\n%s", body)
	}
	if contains(body, "unrelated chore") {
		t.Error("mutation dropped the active search and re-rendered a non-matching row")
	}
}

// searchJSONWithClient is searchJSON with a clientID, so the tab's view prefs
// are stored the way a live browser tab stores them. It asserts the endpoint
// answered and leaves reading the patch to the caller's own request.

// TestLateSearchResponseDoesNotClobberTheBox pins the second half of the
// client-owned rule, on the endpoint the first half was found on.
//
// The search box carries a 300ms debounce and fires @get with the CURRENT
// $searchQuery. If a response echoes the query from its own request's ?q=, a
// response that lands late re-imposes an OLDER value: request ?q=kes was
// issued, the user typed on to "kestrel", and kes's response arrives last and
// sets the box back to "kes". The box owns the signal, so no response may
// write it -- the same clobber the stream handshake was fixed for, reachable
// from /search and from the list endpoint instead.
//
// The box is seeded once per page load from the input's own value= (covered
// separately); here the assertion is only that a patch response stays silent
// about searchQuery.
//
// Red-proof: set SearchQuery from the request in displaySignals again and the
// ?q=kes response re-emits it, failing this test.
func TestLateSearchResponseDoesNotClobberTheBox(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newHandlersFixture(t)
	wireBM25(t, f)
	cookie := f.signup("clobber@example.com")
	owner := f.ownerOf(t, cookie)
	if err := createTodoIndexed(ctx, f.q, f.h.Index, f.h.DB, dbgen.CreateTodoParams{
		ID: dbschema.NewID(), Owner: owner, Title: "kestrel clobber probe",
		Visibility: todo.VisibilityPrivate, Created: dbschema.Now(), Updated: dbschema.Now(),
	}); err != nil {
		t.Fatalf("indexed create: %v", err)
	}

	// Any query works to detect the echo -- what must NOT happen is the
	// response writing the signal at all. (A term the index knows, so the
	// narrowing assertion below is meaningful.)
	body := searchJSON(t, f, cookie, "kestrel").Body.String()
	if contains(body, `"searchQuery"`) {
		t.Errorf("a /search response wrote the client-owned searchQuery signal "+
			"(a late response for an older ?q= would re-impose it over what the "+
			"user has typed since; response was for ?q=kestrel):\n%s", body)
	}
	// The narrowing itself must still happen: silence about the signal is not
	// a licence to skip the search.
	if !contains(body, "kestrel clobber probe") {
		t.Errorf("the response stopped rendering the matching row:\n%s", body)
	}
}

func searchJSONWithClient(
	t *testing.T, f *handlersFixture, cookie *http.Cookie, query, clientID string,
) {
	t.Helper()
	target := "/api/todos/search?q=" + url.QueryEscape(query) + "&filter=all&scope=all&clientID=" + clientID
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d (body: %s)", target, rec.Code, rec.Body.String())
	}
}

func postingCount(t *testing.T, db *sql.DB, owner, docID string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM search_postings WHERE owner = ? AND doc_id = ?`, owner, docID,
	).Scan(&n); err != nil {
		t.Fatalf("count postings: %v", err)
	}
	return n
}

func contains(haystack, needle string) bool { return indexOf(haystack, needle) >= 0 }

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// TestStreamHandshakePreservesSearchAndSlice pins the reconnect contract: the
// handshake must re-list the tab's OWN slice while leaving client-owned
// signals alone.
//
// Both halves were real bugs the browser smoke caught — and the second one
// only surfaced once the first was "fixed". A reconnect re-runs this merge, so
// a handshake that listed all/all re-rendered rows the tab had filtered out,
// and a handshake that echoed SearchQuery reset the search box mid-typing
// (at the time the field had no tag protection, so a stale "" was emitted and
// written over the live text). SearchQuery is `json:"-"` now, so it cannot be
// emitted at all; this asserts the handshake does not set it either.
// Red-proof: echo SearchQuery from the handshake and the first assert fails;
// list all/all and the second does.
func TestStreamHandshakePreservesSearchAndSlice(t *testing.T) {
	t.Parallel()
	f := newHandlersFixture(t)
	wireBM25(t, f)
	cookie := f.signup("streamsearch@example.com")
	me := f.ownerOf(t, cookie)

	if err := createTodoIndexed(context.Background(), f.q, f.h.Index, f.h.DB, dbgen.CreateTodoParams{
		ID: dbschema.NewID(), Owner: me, Title: "kestrel report",
		Visibility: todo.VisibilityPrivate, Created: dbschema.Now(), Updated: dbschema.Now(),
	}); err != nil {
		t.Fatalf("indexed create: %v", err)
	}
	createStamped(t, f, me, "unrelated chore")

	// The tab searches, which stores its query on the stream view.
	const clientID = "tab-streamsearch"
	searchJSONWithClient(t, f, cookie, "kestrel", clientID)

	// Then its stream reconnects (visibility return, blip, Datastar retry).
	srv := httptest.NewServer(f.r)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/todos/stream?clientID="+clientID, nil)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream open: %v", err)
	}
	defer resp.Body.Close()

	buf := make([]byte, 8192)
	readCh := make(chan int, 1)
	go func() {
		n, _ := resp.Body.Read(buf)
		readCh <- n
	}()
	var body string
	select {
	case n := <-readCh:
		body = string(buf[:n])
	case <-time.After(4 * time.Second):
		t.Fatal("stream handshake timed out")
	}
	// The handshake must NOT write $searchQuery. It is bound two-way to the
	// search box, so it is client-owned: the stored view lags what the user
	// typed by the debounce window, and merging that stale value back resets
	// the box — the debounced @get then fires with q empty and lists
	// everything. The field is `json:"-"`, so a handshake that leaves it alone
	// carries no key for it; this asserts the handshake leaves it alone.
	if contains(body, `"searchQuery"`) {
		t.Errorf("stream handshake wrote the client-owned searchQuery signal "+
			"(a reconnect would clobber the search box):\n%s", body)
	}
	if contains(body, "unrelated chore") {
		t.Error("stream handshake listed outside the tab's search slice: " +
			"a reconnect would re-render a non-matching row")
	}
}
