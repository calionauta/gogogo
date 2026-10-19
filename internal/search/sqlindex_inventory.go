// SCOPE:plugin — reading the index's own inventory, for the boot heal.
//
// WHY THIS EXISTS. Postings are a cache of a Go-side pure function
// (search.Stem over search.Tokenize), not a SQL expression, so a row created
// while the bm25 layer was unwired has no postings and NOTHING in SQL can
// compute them. The heal therefore reads the consumer's rows, subtracts the
// ones that already have postings, and re-indexes the difference through the
// same IndexDoc the mutation path uses — which is what makes a healed index
// indistinguishable from an incrementally written one.
//
// The subtraction (rows minus already-indexed) happens in the CONSUMER
// (features/todo/handlers/search_heal.go), not here and not in the root,
// because the consumer is the only place that legitimately holds both halves:
// it owns its rows, and it holds the seam that forwards to this package. This
// package must never learn which table a todo lives in, and the consumer must
// never learn how postings are stored — so this file answers only "what is in
// the index?" and the heal joins that against "what rows exist?".
package search

import (
	"context"
	"fmt"
)

// DocRef identifies one indexed document within one owner's corpus.
//
// Owner is part of the identity, not decoration: two owners can hold a row
// with the same id (ids are generated per insert, so this is theoretical
// today, but the postings table's primary key includes owner, and a heal that
// ignored it would consider owner B's row indexed because owner A's was).
type DocRef struct {
	Owner string
	DocID string
}

// IndexedDocs returns every document that has at least one posting of this
// kind, for every owner.
//
// Deliberately NOT owner-scoped: the heal runs once at boot across the whole
// table, and a per-owner loop would issue one query per user to answer a
// question that one scan answers. This is a maintenance accessor, not a
// search path — the authorization boundary lives on Postings/Stats, which are
// owner-scoped in SQL and are what a query can reach.
func (ix SQLIndex) IndexedDocs(ctx context.Context, kind string) ([]DocRef, error) {
	rows, err := ix.DB.QueryContext(ctx,
		`SELECT DISTINCT owner, doc_id FROM search_postings WHERE kind = ?`, kind)
	if err != nil {
		return nil, fmt.Errorf("search: read indexed docs: %w", err)
	}
	defer rows.Close()
	var out []DocRef
	for rows.Next() {
		var ref DocRef
		if err := rows.Scan(&ref.Owner, &ref.DocID); err != nil {
			return nil, fmt.Errorf("search: scan indexed doc: %w", err)
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}
