// SCOPE:plugin — BM25 over the SQL index, end to end.
package search

import (
	"context"
	"testing"
)

// TestSQLIndexRanksThroughTheTransaction pins the layer end to end: postings
// written inside a transaction, then ranked by BM25 the way a consumer would
// ask. Red-proof: drop the delete in IndexDoc (re-indexing duplicates terms)
// or write outside the tx (rows vanish) and this changes.
func TestSQLIndexRanksThroughTheTransaction(t *testing.T) {
	t.Parallel()
	sqlDB := openSearchDB(t)
	ix := SQLIndex{DB: sqlDB}
	seed(t, ix, sqlDB, "owner-a", map[string]string{
		"1": "Ship the smoke guard",
		"2": "Read the parser diff",
		"3": "Write the handoff note",
	})

	bm := BM25{Index: ix}
	hits, err := bm.Search(context.Background(), Query{Owner: "owner-a", Text: "smoke", Field: "title"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "1" {
		t.Fatalf("hits = %+v, want only row 1", hits)
	}

	// Re-indexing the same document must replace its postings, not stack
	// them: that is the primary key plus the delete, and it is what makes a
	// title edit safe.
	editErr := ix.IndexDoc(context.Background(), sqlDB, "owner-a", "todo", "1", "title", "Invoice the client")
	if editErr != nil {
		t.Fatalf("re-index: %v", editErr)
	}
	old, oldErr := bm.Search(context.Background(), Query{Owner: "owner-a", Text: "smoke", Field: "title"})
	if oldErr != nil {
		t.Fatalf("search after edit: %v", oldErr)
	}
	if len(old) != 0 {
		t.Errorf("hits = %+v, want none: the old title must stop matching", old)
	}
}
