// SCOPE:plugin — search: one seam, three layers, no external engine.
//
// To remove: delete internal/search + the capability entry + the wiring in
// internal/server/chi.go. A consumer that loses it keeps working with the
// SQL-only substring layer (features/todo shows that degrade).
//
// Why not FTS5 or Bleve: the engine behind this app's driver has no FTS5
// module and no Turso native FTS compiled into the published Go binding
// (verified across tursogo v0.8.2/v0.8.3/v0.9.0-pre.1: `unknown module
// name 'fts'`), and a separate index store (Bleve/bluge) would put the
// truth of a row in two places — the rule this repo pays for everywhere.
// So the layers are ours, in the same database file and the same
// transaction as the rows they index, and the heaviest one can be switched
// off without touching a consumer.
package search

import (
	"context"
	"strings"
	"unicode"
)

// Hit is one result: the row's id, its score, and where the query matched
// (byte offsets into the matched field) so a caller can highlight without
// re-running the match.
type Hit struct {
	ID    string
	Score float64
	Spans []Span
}

// Span is one matched byte range inside the searched field.
type Span struct {
	Start, End int
}

// Query is what a consumer asks for. Field names are the indexed field
// names ("title" today), so a consumer with more than one field indexes
// both and reads hits for either.
type Query struct {
	Owner string
	Text  string
	Field string
	Limit int
	// IncludePublic widens the corpus to rows the consumer marks public.
	// The owner scope stays: a private row of another owner is never a hit.
	IncludePublic bool
	Target        string
}

// Searcher is the whole contract a consumer sees. The three layers differ
// only in how they fill Hits: the SQL substring layer, the in-Go fuzzy
// matcher, and the BM25 postings layer.
type Searcher interface {
	Search(ctx context.Context, q Query) ([]Hit, error)
}

// Tokenize normalizes one field for matching and indexing: lowercase,
// letters and digits only, split on everything else. Shared by all three
// layers so a fuzzy match and a postings lookup can never disagree about
// where a word starts.
func Tokenize(s string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}
