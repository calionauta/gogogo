// SCOPE:plugin — the data seams the search layers sit on.
package search

import "context"

// Doc is one row as the layers see it: an id and the indexed fields. The
// layers never touch a table — they score Docs their Source hands them,
// which is what keeps the matchers pure and testable without a database.
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

// Source is what a CONSUMER provides: its own rows, and the SQL that can
// filter them cheaply. The plugin never learns which table a todo lives in;
// the consumer never learns how postings are stored. Where the two meet is
// the field name ("title"), which both sides spell the same way.
type Source interface {
	// Candidates returns the owner's rows for the queried field. The
	// in-memory layers (fuzzy) score these.
	Candidates(ctx context.Context, q Query) ([]Doc, error)
	// Substring runs the cheapest layer in the consumer's own SQL, where
	// the index it already has can help: no ranking, no in-Go scoring.
	Substring(ctx context.Context, q Query) ([]Hit, error)
}

// Index is what the PLUGIN provides: the inverted index, scoped by owner and
// field so a query cannot widen an authorization it was not given.
type Index interface {
	// Postings returns the index rows for terms in the queried field.
	Postings(ctx context.Context, q Query, terms []string) ([]Posting, error)
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
