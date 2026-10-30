// SCOPE:plugin — the index's authorization boundary, in SQL.
package search

import (
	"context"
	"database/sql"
	"testing"
)

// TestSQLIndexIsOwnerScoped is the security half: the same title under two
// owners must not leak across, and the count BM25 uses must be this owner's
// corpus, not everyone's. Red-proof: drop `owner = ?` from the postings or
// stats query and both assertions fail.
func TestSQLIndexIsOwnerScoped(t *testing.T) {
	t.Parallel()
	sqlDB := openSearchDB(t)
	ix := SQLIndex{DB: sqlDB}
	ctx := context.Background()
	seed(t, ix, sqlDB, "owner-a", map[string]string{"1": "Ship the smoke guard"})
	seed(t, ix, sqlDB, "owner-b", map[string]string{"9": "Ship the smoke guard too"})

	bm := BM25{Index: ix}
	hits, err := bm.Search(ctx, Query{Owner: "owner-a", Text: "smoke", Field: "title"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "1" {
		t.Fatalf("hits = %+v, want only owner-a's row", hits)
	}

	stats, err := ix.Stats(ctx, Query{Owner: "owner-a", Field: "title"})
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Docs != 1 {
		t.Errorf("corpus = %d docs, want 1 (owner-a's own)", stats.Docs)
	}
}

// TestSQLIndexIncludesPublicOnlyWhenAsked pins the shared-search exception:
// a public row of another owner is a hit only when the query asks for it, and
// a private row of another owner never is. Red-proof: drop the visibility
// predicate from Postings and the private row leaks; drop IncludePublic and
// the shared row disappears.
func TestSQLIndexIncludesPublicOnlyWhenAsked(t *testing.T) {
	t.Parallel()
	sqlDB := openSearchDB(t)
	ix := SQLIndex{DB: sqlDB}
	ctx := context.Background()
	seedVisible(t, ix, sqlDB, "owner-a", "1", "private smoke plan", "private")
	seedVisible(t, ix, sqlDB, "owner-b", "9", "shared smoke plan", "public")
	seedVisible(t, ix, sqlDB, "owner-b", "10", "secret smoke plan", "private")

	bm := BM25{Index: ix}
	own, err := bm.Search(ctx, Query{Owner: "owner-a", Text: "smoke", Field: "title"})
	if err != nil {
		t.Fatalf("own search: %v", err)
	}
	if len(own) != 1 || own[0].ID != "1" {
		t.Fatalf("own hits = %+v, want only owner-a's row", own)
	}
	shared, err := bm.Search(ctx, Query{Owner: "owner-a", Text: "smoke", Field: "title", IncludePublic: true})
	if err != nil {
		t.Fatalf("shared search: %v", err)
	}
	if len(shared) != 2 {
		t.Fatalf("shared hits = %+v, want owner-a + owner-b public", shared)
	}
	for _, h := range shared {
		if h.ID == "10" {
			t.Fatal("another owner's private row leaked into the shared search")
		}
	}
	stats, err := ix.Stats(ctx, Query{Owner: "owner-a", Field: "title", IncludePublic: true})
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Docs != 2 {
		t.Errorf("corpus = %d docs, want own + public", stats.Docs)
	}
}

func seedVisible(t *testing.T, ix SQLIndex, sqlDB *sql.DB, owner, id, title, visibility string) {
	t.Helper()
	ctx := context.Background()
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("tx: %v", err)
	}
	if err := ix.IndexDoc(ctx, tx, owner, "todo", id, "title", title, visibility); err != nil {
		t.Fatalf("index %s: %v", id, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}
