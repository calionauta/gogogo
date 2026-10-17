// SCOPE:plugin — the inverted index in SQL: the plugin's own half.
package search

import (
	"context"
	"database/sql"
)

// Execer is the write seam. A consumer passes its own *sql.Tx, so index
// writes ride the same transaction as the row they describe: no index entry
// without the row, no row without its index entry — the property that made
// the engine's native FTS attractive, kept without the engine.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// SQLIndex is the postings index: a delete plus one insert per term on a
// write, one query per search. It implements Index, and BM25 mode is built
// on it.
type SQLIndex struct {
	DB Execer
}

// DeleteDoc removes every posting for one document, all fields, so a deleted
// or renamed row cannot keep matching.
func (ix SQLIndex) DeleteDoc(ctx context.Context, ex Execer, owner, kind, docID string) error {
	_, err := ex.ExecContext(ctx,
		`DELETE FROM search_postings WHERE owner = ? AND kind = ? AND doc_id = ?`,
		owner, kind, docID)
	return err
}
