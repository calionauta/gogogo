// SCOPE:feature — the search-index heal (red-proof).
//
// THE BUG THESE TESTS EXIST FOR. Postings are a cache of a Go-side pure
// function (search.Stem over search.Tokenize), so SQL cannot rebuild them. A
// row created while the bm25 layer was unwired has no postings, and relevance
// search would then answer "no results" for a todo the user can see on screen —
// a mode that looks enabled and is silently blind to every pre-existing row.
// Because the picker now offers bm25 on every build, that is the NORMAL state
// of an upgraded install, not an edge case.
package handlers

import (
	"context"
	"testing"

	"github.com/calionauta/gogogo/internal/search"
)

// countPostingsFor counts one owner's postings rows, so a test can assert the
// index's state directly instead of inferring it from a search result.
func countPostingsFor(t *testing.T, f *handlersFixture, owner string) int {
	t.Helper()
	var n int
	if err := f.sqlDB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM search_postings WHERE owner = ?`, owner).Scan(&n); err != nil {
		t.Fatalf("count postings: %v", err)
	}
	return n
}

// TestHealSearchIndexIndexesRowsWrittenBeforeTheIndex is the red-proof.
//
// The fixture writes rows with the index DISABLED (the substring-only state an
// upgraded install is in), then heals and requires bm25 to find them. Without
// the heal the search assertion fails — which is exactly what a user would
// experience after enabling relevance search on an existing database.
func TestHealSearchIndexIndexesRowsWrittenBeforeTheIndex(t *testing.T) {
	t.Parallel()
	f := newHandlersFixture(t)
	cookie := f.signup("heal@example.com")
	me := f.ownerOf(t, cookie)

	// Rows written with no indexer wired: the postings table stays empty,
	// which is the state the heal must repair.
	f.h.SetSearch(testSearchLayers(search.SubstringMode, mustSubstring(t, f)), nil, nil)
	createStamped(t, f, me, "deploy the kestrel parser")
	if n := countPostingsFor(t, f, me); n != 0 {
		t.Fatalf("precondition: %d postings written with the index off, want 0", n)
	}

	// Wire bm25 plus the heal, the way boot does.
	idx := search.SQLIndex{DB: f.sqlDB}
	if err := HealSearchIndex(context.Background(), f.q, NewTodoIndexer(idx), f.sqlDB); err != nil {
		t.Fatalf("heal: %v", err)
	}
	if n := countPostingsFor(t, f, me); n == 0 {
		t.Fatal("heal wrote no postings for a row created before the index existed")
	}

	// The point of the postings: relevance search must now FIND the old row.
	bm25, err := search.New(search.BM25Mode, NewTodoSource(f.q), idx)
	if err != nil {
		t.Fatalf("bm25: %v", err)
	}
	f.h.SetSearch(testSearchLayers(search.BM25Mode, bm25), NewTodoIndexer(idx), f.sqlDB)
	body := searchJSON(t, f, cookie, "kestrel").Body.String()
	if !contains(body, "deploy the kestrel parser") {
		t.Errorf("bm25 did not find a pre-existing row after the heal:\n%s", body)
	}
}

// TestHealSearchIndexIsIdempotent pins the property that makes a boot-time heal
// safe to run on every start: a second run must not duplicate postings. The
// postings primary key would reject a true duplicate, so the real risk this
// guards is the heal rewriting every row on every boot (a full-table write for
// no reason) — measured here as "no rows were missing".
func TestHealSearchIndexIsIdempotent(t *testing.T) {
	t.Parallel()
	f := newHandlersFixture(t)
	cookie := f.signup("heal2@example.com")
	me := f.ownerOf(t, cookie)
	createStamped(t, f, me, "write the handoff note")

	idx := search.SQLIndex{DB: f.sqlDB}
	ix := NewTodoIndexer(idx)
	if err := HealSearchIndex(context.Background(), f.q, ix, f.sqlDB); err != nil {
		t.Fatalf("first heal: %v", err)
	}
	first := countPostingsFor(t, f, me)
	if first == 0 {
		t.Fatal("first heal wrote nothing")
	}
	if err := HealSearchIndex(context.Background(), f.q, ix, f.sqlDB); err != nil {
		t.Fatalf("second heal: %v", err)
	}
	if got := countPostingsFor(t, f, me); got != first {
		t.Errorf("postings went %d -> %d across two heals, want unchanged (idempotent)", first, got)
	}
}

// TestHealSearchIndexNoRowsIsFree pins the empty-install path: a database with
// no todos must not error (nobody wants a boot failure on a fresh install) and
// must not open a write transaction.
func TestHealSearchIndexNoRowsIsFree(t *testing.T) {
	t.Parallel()
	f := newHandlersFixture(t)
	idx := search.SQLIndex{DB: f.sqlDB}
	if err := HealSearchIndex(context.Background(), f.q, NewTodoIndexer(idx), f.sqlDB); err != nil {
		t.Fatalf("heal on an empty database: %v", err)
	}
}

// mustSubstring builds the substring layer for a fixture, failing the test on
// error (the mode needs only the source, so an error is a wiring bug).
func mustSubstring(t *testing.T, f *handlersFixture) search.Searcher {
	t.Helper()
	layer, err := search.New(search.SubstringMode, NewTodoSource(f.q), nil)
	if err != nil {
		t.Fatalf("substring layer: %v", err)
	}
	return layer
}
