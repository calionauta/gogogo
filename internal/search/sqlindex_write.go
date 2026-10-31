// SCOPE:plugin — writing postings, split from sqlindex.go for the file gate.
package search

import (
	"context"
	"fmt"
)

// IndexDoc replaces one document's postings for one field, inside the
// consumer's transaction. An empty field writes none (and the delete above
// already cleared the old ones), which is how a cleared title stops
// matching.
func (ix SQLIndex) IndexDoc(
	ctx context.Context, ex Execer, owner, kind, docID, field, text, visibility string,
) error {
	if err := ix.DeleteDoc(ctx, ex, owner, kind, docID); err != nil {
		return fmt.Errorf("search: clear postings for %s/%s: %w", kind, docID, err)
	}
	tokens := Tokenize(text)
	if len(tokens) == 0 {
		return nil
	}
	// indexLen is the RAW token count: BM25's length normalization measures the
	// document, which really has that many words. The TERM is folded instead,
	// because the term column is the persisted key and a folded query can only
	// find a folded key.
	indexLen := len(tokens)
	tokens = stemAll(tokens)
	counts := make(map[string]int, len(tokens))
	for _, t := range tokens {
		counts[t]++
	}
	for term, n := range counts {
		if _, err := ex.ExecContext(ctx,
			`INSERT INTO search_postings
			   (owner, kind, doc_id, field, term, count, length, visibility)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			owner, kind, docID, field, term, n, indexLen, visibility,
		); err != nil {
			return fmt.Errorf("search: index %s/%s term %q: %w", kind, docID, term, err)
		}
	}
	return nil
}
