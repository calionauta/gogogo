// SCOPE:plugin — the fuzzy layer: no index, no extra write, typo tolerant.
package search

import (
	"context"
	"sort"
	"strings"
)

// Fuzzy scores candidates in memory. It serves the case the SQL substring
// layer cannot and a full inverted index is not worth maintaining: a
// handful of rows per owner, queried with a misspelling.
//
// The tiers, best first, ARE the algorithm:
//
//	1.00  the normalized query appears in the field verbatim (contiguous)
//	0.95  every query token IS a field token, just not contiguous ("smoke guard")
//	0.90  every query token is a PREFIX of a field token ("parse dif" -> "parser", "diff")
//	0.80  every query token is within ONE edit of a field token ("smoek" -> "smoke")
//	0.75  every query token is a SUBSEQUENCE of a field token, or vice versa,
//	      guarded by a length floor and ratio ("handooff" contains "handoff")
//	0.00  some token matched nothing
//
// The score is the WEAKEST tier among the query's tokens: a query is only
// as good as the token it had to stretch for. Ties break on the earliest
// match, so equally good rows keep their list order.
type Fuzzy struct {
	Source Source
}

// Search implements Searcher.
func (f Fuzzy) Search(ctx context.Context, q Query) ([]Hit, error) {
	docs, err := f.Source.Candidates(ctx, q)
	if err != nil {
		return nil, err
	}
	terms := Tokenize(q.Text)
	if len(terms) == 0 {
		return nil, nil
	}
	hits := make([]Hit, 0, len(docs))
	for _, doc := range docs {
		if hit, ok := scoreDoc(doc.ID, q.Text, terms, doc.Fields[q.Field]); ok {
			hits = append(hits, hit)
		}
	}
	sortHits(hits)
	return limitHits(hits, q.Limit), nil
}

// sortHits orders by score, then by the first match position (a Slice is
// already in that order, so only the score comparison is unstable work).
func sortHits(hits []Hit) {
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
}

func limitHits(hits []Hit, limit int) []Hit {
	if limit > 0 && len(hits) > limit {
		return hits[:limit]
	}
	return hits
}

// normalize is the string both the substring tier and the subsequence tier
// compare: lowercase, single spaces, no punctuation edges.
func normalize(s string) string {
	return strings.Join(Tokenize(s), " ")
}
