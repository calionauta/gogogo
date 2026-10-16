// SCOPE:plugin — the index's authorization boundary, in SQL.
package search

import (
	"context"
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
