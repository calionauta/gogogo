// SCOPE:plugin — the BM25 layer: an inverted index in SQL, scored in Go.
package search

import (
	"context"
	"sort"
)

// BM25 constants: what every reference implementation uses (Lucene's
// defaults). k1 saturates term frequency, b normalizes by length.
const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// BM25 is the third layer: postings from the store, no scan of the corpus.
// It earns its extra write path (one postings row per term) exactly when the
// fuzzy layer's weakness starts to matter — a corpus big enough that a
// common word matching every row is worse than a slower query.
//
// Per query term t and document D:
//
//	score += IDF(t) * f(t,D)(k1+1) / ( f(t,D) + k1(1 - b + b*len(D)/avgdl) )
//	IDF(t)  = ln( 1 + (N - n(t) + 0.5) / (n(t) + 0.5) )
//
// Two properties no substring match has: a RARE term outranks a common one
// (IDF), and a short document stating the term once outranks a long one
// repeating it (saturation + length normalization).
type BM25 struct {
	Index Index
}

// Search implements Searcher. A document matching only SOME terms still
// scores — BM25 sums per term — which is the deliberate difference from the
// fuzzy layer's AND semantics: that is what ranking means.
func (b BM25) Search(ctx context.Context, q Query) ([]Hit, error) {
	terms := stemAll(Tokenize(q.Text))
	if len(terms) == 0 {
		return nil, nil
	}
	postings, err := b.Index.Postings(ctx, q, terms)
	if err != nil {
		return nil, err
	}
	stats, err := b.Index.Stats(ctx, q)
	if err != nil {
		return nil, err
	}
	if stats.Docs == 0 || len(postings) == 0 {
		return nil, nil
	}
	hits := scoreDocs(terms, groupPostings(postings), stats)
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	return limitHits(hits, q.Limit), nil
}
