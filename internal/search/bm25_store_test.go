// SCOPE:plugin — the test store BM25 needs (postings + corpus stats).
package search

import (
	"context"
	"strings"
)

// indexedStore is memStore plus the postings a real store would hold. The
// test indexes the corpus the way the write path does — one posting per
// term per document — so the numbers under test are the numbers production
// would produce.
type indexedStore struct {
	docs []Doc
	post []Posting
	stat CorpusStats
}

// indexDocs builds the postings a real store would hold for the corpus.
//
// The field is not a parameter: every caller indexes "title" (the only indexed
// field the toy corpus has), and a parameter that is constant at every call
// site is a constant (unparam is right).
func indexDocs(docs []Doc) indexedStore {
	const field = "title"
	s := indexedStore{docs: docs}
	var total int
	for _, d := range docs {
		tokens := Tokenize(d.Fields[field])
		total += len(tokens)
		// Fold like production (sqlindex_write) does: the term is the folded
		// key, the length is the raw token count. A test store indexing
		// UNFOLDED terms would leave the write path untested and pass while
		// the real index failed to match.
		length := len(tokens)
		tokens = stemAll(tokens)
		counts := map[string]int{}
		for _, t := range tokens {
			counts[t]++
		}
		for term, n := range counts {
			s.post = append(s.post, Posting{DocID: d.ID, Field: field, Term: term, Count: n, Length: length})
		}
	}
	s.stat = CorpusStats{Docs: len(docs), AvgLen: float64(total) / float64(len(docs))}
	return s
}

func (s indexedStore) Candidates(context.Context, Query) ([]Doc, error) { return s.docs, nil }

func (s indexedStore) Postings(_ context.Context, _ Query, terms []string) ([]Posting, error) {
	want := map[string]bool{}
	for _, t := range terms {
		want[t] = true
	}
	var out []Posting
	for _, p := range s.post {
		if want[p.Term] {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s indexedStore) Stats(context.Context, Query) (CorpusStats, error) { return s.stat, nil }

// Substring is the SQL layer's job in production; the test store answers it
// the naive way so a layer test never needs a database.
func (s indexedStore) Substring(_ context.Context, q Query) ([]Hit, error) {
	var out []Hit
	for _, d := range s.docs {
		if strings.Contains(strings.ToLower(d.Fields[q.Field]), strings.ToLower(q.Text)) {
			out = append(out, Hit{ID: d.ID, Score: tierExact})
		}
	}
	return out, nil
}
