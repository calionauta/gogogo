// SCOPE:plugin — BM25's per-document math, split for the file gate.
package search

import "math"

// docTerms is one document's matched terms as the scorer needs them: the
// term frequencies plus the field's length for the normalization.
type docTerms struct {
	counts map[string]int
	length int
}

// groupPostings folds the postings rows into per-document term counts.
func groupPostings(postings []Posting) map[string]docTerms {
	docs := make(map[string]docTerms, len(postings))
	for _, p := range postings {
		d, ok := docs[p.DocID]
		if !ok {
			d = docTerms{counts: map[string]int{}, length: p.Length}
		}
		d.counts[p.Term] += p.Count
		docs[p.DocID] = d
	}
	return docs
}

// scoreDocs ranks every document the postings touched.
func scoreDocs(terms []string, docs map[string]docTerms, stats CorpusStats) []Hit {
	// Document frequency per term, from the postings themselves: n(t) is how
	// many documents carry t, and it is the only corpus-wide number BM25
	// needs beyond the document count.
	df := map[string]int{}
	for _, d := range docs {
		for term := range d.counts {
			df[term]++
		}
	}
	hits := make([]Hit, 0, len(docs))
	for id, d := range docs {
		var score float64
		for _, term := range terms {
			tf := d.counts[term]
			if tf == 0 {
				continue
			}
			score += idf(stats.Docs, df[term]) * saturate(tf, d.length, stats.AvgLen)
		}
		if score > 0 {
			hits = append(hits, Hit{ID: id, Score: score})
		}
	}
	return hits
}

// idf is the BM25 inverse document frequency, in its non-negative form
// (the +1 inside the log keeps a term present in every document positive
// rather than zero, which is what Lucene does and what makes a two-word
// query still rank).
func idf(n, df int) float64 {
	return math.Log(1 + (float64(n)-float64(df)+0.5)/(float64(df)+0.5))
}

// saturate is the term-frequency term: it grows with tf but flattens out,
// and shrinks for documents longer than the corpus average.
func saturate(tf, length int, avgLen float64) float64 {
	if avgLen <= 0 {
		avgLen = 1
	}
	norm := 1 - bm25B + bm25B*float64(length)/avgLen
	return float64(tf) * (bm25K1 + 1) / (float64(tf) + bm25K1*norm)
}
