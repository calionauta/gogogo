// SCOPE:plugin — BM25's ranking contract on the example corpus.
package search

import (
	"context"
	"math"
	"testing"
)

// TestBM25ScoreIsTheFormula pins the arithmetic rather than a vibe: for the
// example corpus (N=4, avgdl=3.75) a single-occurrence term in one of four
// rows scores IDF(1 of 4) * saturation(1 occurrence, len 4, avg 3.75) =
// 1.204 * 0.973 = 1.172. Red-proof: change k1/b or drop the length
// normalization and this goes red with a number — which is the point of
// asserting a number.
func TestBM25ScoreIsTheFormula(t *testing.T) {
	t.Parallel()
	b := BM25{Index: indexDocs(exampleCorpus().docs, "title")}
	hits, err := b.Search(context.Background(), Query{Text: "smoke", Field: "title"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "1" {
		t.Fatalf("hits = %+v, want only row 1", hits)
	}
	if want := 1.172; math.Abs(hits[0].Score-want) > 0.01 {
		t.Errorf("score = %.3f, want %.3f", hits[0].Score, want)
	}
}

// TestBM25RareTermOutranksCommonTerm is the property that justifies the
// whole layer: "the" appears in three of four rows, "invoice" in one, so
// the ranking of the query that has both must be driven by the rare one.
// A substring matcher cannot express this at all — it would rank the rows
// by position.
func TestBM25RareTermOutranksCommonTerm(t *testing.T) {
	t.Parallel()
	b := BM25{Index: indexDocs(exampleCorpus().docs, "title")}
	hits, err := b.Search(context.Background(), Query{Text: "the invoice", Field: "title"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 || hits[0].ID != "4" {
		t.Fatalf("hits = %+v, want row 4 first (the rare term wins)", hits)
	}
	// The common term alone must score lower than the rare one alone.
	common, err := b.Search(context.Background(), Query{Text: "the", Field: "title"})
	if err != nil {
		t.Fatalf("search the: %v", err)
	}
	rare, err := b.Search(context.Background(), Query{Text: "invoice", Field: "title"})
	if err != nil {
		t.Fatalf("search invoice: %v", err)
	}
	if common[0].Score >= rare[0].Score {
		t.Errorf("common score %.3f >= rare %.3f, want IDF to separate them", common[0].Score, rare[0].Score)
	}
}
