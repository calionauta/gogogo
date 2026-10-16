// SCOPE:plugin — the row-access seam every search layer shares.
package search

import "context"

// Doc is one row as the layers see it: an id and the indexed fields. The
// layers never touch a table — they score Docs the Store hands them, which
// is what keeps the matchers pure and testable without a database.
type Doc struct {
	ID     string
	Fields map[string]string
}

// Posting is one term in one document, as the inverted index stores it.
type Posting struct {
	DocID  string
	Field  string
	Term   string
	Count  int
	Length int // the field's token count, for BM25's length normalization
}

// Store is the only thing a layer needs from the world: the candidate rows
// for a query, and (for BM25) the postings for its terms. Both are
// owner-scoped, so a layer cannot widen an authorization it did not get.
type Store interface {
	// Candidates returns the owner's rows for the queried field.
	Candidates(ctx context.Context, q Query) ([]Doc, error)
	// Postings returns the index rows for terms in the queried field.
	Postings(ctx context.Context, q Query, terms []string) ([]Posting, error)
	// Substring runs the cheapest layer in SQL, where the index the caller
	// already has can help: no ranking, no in-Go scoring.
	Substring(ctx context.Context, q Query) ([]Hit, error)
	// Stats returns the corpus size BM25 needs: the document count and the
	// average field length. Document frequency is derived from the postings
	// themselves, so only these two need their own query.
	Stats(ctx context.Context, q Query) (CorpusStats, error)
}

// CorpusStats is what BM25's ranking needs beyond the postings.
type CorpusStats struct {
	Docs   int
	AvgLen float64
}
