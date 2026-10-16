// SCOPE:plugin — the fuzzy tier contract, on the example corpus.
package search

import (
	"context"
	"testing"
)

// memStore is the Store seam in memory: the fuzzy layer never needs a DB.
type memStore struct {
	docs []Doc
}

func (m memStore) Candidates(context.Context, Query) ([]Doc, error) { return m.docs, nil }

func (m memStore) Postings(context.Context, Query, []string) ([]Posting, error) { return nil, nil }

func (m memStore) Stats(context.Context, Query) (CorpusStats, error) { return CorpusStats{}, nil }

func (m memStore) Substring(context.Context, Query) ([]Hit, error) { return nil, nil }

// exampleCorpus is the four todos used to explain the tiers.
func exampleCorpus() memStore {
	rows := map[string]string{
		"1": "Ship the smoke guard",
		"2": "Read the parser diff",
		"3": "Write the handoff note",
		"4": "Pay the invoice",
	}
	docs := make([]Doc, 0, len(rows))
	for id, title := range rows {
		docs = append(docs, Doc{ID: id, Fields: map[string]string{"title": title}})
	}
	return memStore{docs: docs}
}

// TestFuzzyTiers is the contract from the tiers doc comment: each query
// reaches exactly the row and the tier the explanation promised.
func TestFuzzyTiers(t *testing.T) {
	t.Parallel()
	f := Fuzzy{Store: exampleCorpus()}
	for name, tc := range map[string]struct {
		query string
		want  string
		score float64
	}{
		"verbatim":     {query: "smoke", want: "1", score: tierExact},
		"typo":         {query: "smoek", want: "1", score: tierEdit},
		"typo-second":  {query: "invioce", want: "4", score: tierEdit},
		"token-prefix": {query: "parse dif", want: "2", score: tierPrefix},
		"doubled":      {query: "handooff", want: "3", score: tierEdit},
	} {
		hits, err := f.Search(context.Background(), Query{Text: tc.query, Field: "title", Limit: 5})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(hits) != 1 || hits[0].ID != tc.want {
			t.Errorf("%s (%q): hits = %+v, want just row %s", name, tc.query, hits, tc.want)
			continue
		}
		if hits[0].Score != tc.score {
			t.Errorf("%s (%q): score = %.2f, want %.2f", name, tc.query, hits[0].Score, tc.score)
		}
	}
}

// TestFuzzyRejectsUnmatchedToken pins the AND semantics: a query is only as
// good as its weakest token, so "smoke invoice" matches nothing rather than
// ranking the two rows that each match half of it.
func TestFuzzyRejectsUnmatchedToken(t *testing.T) {
	t.Parallel()
	f := Fuzzy{Store: exampleCorpus()}
	hits, err := f.Search(context.Background(), Query{Text: "smoke invoice", Field: "title"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("hits = %+v, want none (no row has both tokens)", hits)
	}
}
