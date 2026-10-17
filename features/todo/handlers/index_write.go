// SCOPE:feature — the todo index writer: the postings half of every mutation
// that can change which rows a search matches.
//
// Both write paths go through here — the HTTP handlers and the cross-instance
// CRUD bridge — because a second path that skipped the index would recreate
// exactly the row/index divergence the transactional design exists to
// prevent. Split from mutations.go for the file-size gate. To remove: delete
// with features/todo/ (the plugin's own half lives in internal/search).
package handlers

import (
	"context"
	"database/sql"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// indexEnabled reports whether postings must be kept in step with the rows.
// False is the common case: the substring and fuzzy layers answer from the
// rows themselves, so a search box in those modes costs zero extra writes.
func indexEnabled(ix SearchIndexer, db *sql.DB) bool { return ix != nil && db != nil }

// createTodoIndexed inserts a row and its postings in one transaction, so a
// row can never exist without the index entry a bm25 search would need, nor
// an index entry outlive a failed insert.
func createTodoIndexed(ctx context.Context, q *dbgen.Queries, ix SearchIndexer, db *sql.DB,
	p dbgen.CreateTodoParams,
) error {
	if !indexEnabled(ix, db) {
		_, err := q.CreateTodo(ctx, p)
		return err
	}
	return dbschema.Transact(ctx, db, "todo create+index", func(ctx context.Context, tx *sql.Tx) error {
		if _, err := q.WithTx(tx).CreateTodo(ctx, p); err != nil {
			return err
		}
		return ix.IndexTodo(ctx, tx, p.Owner, p.ID, p.Title)
	})
}

// deleteTodoIndexed removes a row and its postings in one transaction.
func deleteTodoIndexed(ctx context.Context, q *dbgen.Queries, ix SearchIndexer, db *sql.DB,
	owner, id string,
) error {
	if !indexEnabled(ix, db) {
		return q.DeleteTodoByID(ctx, id)
	}
	return dbschema.Transact(ctx, db, "todo delete+index", func(ctx context.Context, tx *sql.Tx) error {
		if err := ix.DeleteTodo(ctx, tx, owner, id); err != nil {
			return err
		}
		return q.WithTx(tx).DeleteTodoByID(ctx, id)
	})
}

// clearCompletedIndexed deletes an owner's completed rows and their postings
// in one transaction. The ids are read inside the same transaction: reading
// them outside would race a concurrent toggle into clearing a row whose
// postings were written after the read, leaving an orphan index entry.
func clearCompletedIndexed(ctx context.Context, q *dbgen.Queries, ix SearchIndexer, db *sql.DB,
	owner string,
) (int64, error) {
	if !indexEnabled(ix, db) {
		return q.DeleteCompletedByOwner(ctx, owner)
	}
	var count int64
	err := dbschema.Transact(ctx, db, "todo clear+index", func(ctx context.Context, tx *sql.Tx) error {
		txq := q.WithTx(tx)
		ids, err := txq.ListCompletedIDsByOwner(ctx, owner)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if derr := ix.DeleteTodo(ctx, tx, owner, id); derr != nil {
				return derr
			}
		}
		count, err = txq.DeleteCompletedByOwner(ctx, owner)
		return err
	})
	return count, err
}
