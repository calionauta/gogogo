// SCOPE:plugin — reading postings and corpus stats (BM25's inputs).
package search

import (
	"context"
	"fmt"
	"strings"
)

// Postings implements Index: the rows for the queried terms, owner- and
// field-scoped in SQL so a search cannot see another owner's corpus.
func (ix SQLIndex) Postings(ctx context.Context, q Query, terms []string) ([]Posting, error) {
	if len(terms) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(terms)+2)
	args = append(args, q.Owner, q.Field)
	placeholders := make([]string, 0, len(terms))
	for _, t := range terms {
		placeholders = append(placeholders, "?")
		args = append(args, t)
	}
	rows, err := ix.DB.QueryContext(ctx,
		`SELECT doc_id, term, count, length FROM search_postings
		 WHERE owner = ? AND field = ? AND term IN (`+strings.Join(placeholders, ", ")+`)`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("search: read postings: %w", err)
	}
	defer rows.Close()
	var out []Posting
	for rows.Next() {
		p := Posting{Field: q.Field}
		if err := rows.Scan(&p.DocID, &p.Term, &p.Count, &p.Length); err != nil {
			return nil, fmt.Errorf("search: scan posting: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Stats implements Index: the corpus size and the average field length, over
// every indexed document for this owner and field (not just the matching
// ones — an IDF denominator that only counted matches would be meaningless).
func (ix SQLIndex) Stats(ctx context.Context, q Query) (CorpusStats, error) {
	var out CorpusStats
	var avg *float64
	err := ix.DB.QueryRowContext(ctx,
		`SELECT COUNT(*), AVG(length) FROM (
		   SELECT DISTINCT doc_id, length FROM search_postings
		    WHERE owner = ? AND field = ?)`,
		q.Owner, q.Field).Scan(&out.Docs, &avg)
	if err != nil {
		return out, fmt.Errorf("search: corpus stats: %w", err)
	}
	if avg != nil {
		out.AvgLen = *avg
	}
	return out, nil
}
