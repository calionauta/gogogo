// SCOPE:feature — the todo feature's search-index heal, run once at boot.
//
// WHY THIS EXISTS. The postings table is a CACHE of a Go-side pure function
// (search.Stem over search.Tokenize), not a SQL expression, so it cannot be
// rebuilt by a migration. A row created while the bm25 layer was unwired has
// no postings, and relevance search would silently find NOTHING for every
// pre-existing todo — a mode that looks enabled and answers "no results" for
// data the user can see on screen. Because the picker now offers bm25 on every
// build (including installs that previously ran substring-only, where every
// row predates the index), that gap is the normal case on upgrade, not an edge
// case.
//
// The heal is IDEMPOTENT and CHEAP WHEN THERE IS NOTHING TO DO: it reads the
// index's inventory once, reads the rows once, and does nothing when they
// agree. That is what makes it safe on every boot rather than a one-time
// migration step nobody would remember to run — and it also repairs the other
// ways a posting can go missing (a transaction rolled back by a crash, a row
// restored from a backup taken before the index existed).
//
// It deliberately does NOT delete postings for rows that no longer exist:
// orphaned entries cannot match anything (every hit is intersected with the
// caller's visible set in narrowBySearch), so they are inert, and a boot-time
// sweep that deletes rows is a far worse failure mode than a stale key if the
// row read is ever wrong.
//
// To remove: delete with features/todo/ (the plugin's own half lives in
// internal/search).
package handlers

import (
	"context"
	"database/sql"
	"fmt"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/search"
)

// HealSearchIndex re-indexes every row whose postings are missing, so the bm25
// layer answers for the whole corpus instead of only for rows touched since the
// index was wired.
//
// Takes the same three dependencies as the indexed write paths in
// index_write.go (queries, indexer, pool) and for the same reason: the rows
// and their postings must be written through one transaction, which needs the
// pool the handler holds. Returns nil when there is nothing to do.
func HealSearchIndex(ctx context.Context, q *dbgen.Queries, ix TodoIndexer, db *sql.DB) error {
	rows, err := q.ListTodosForIndex(ctx)
	if err != nil {
		return fmt.Errorf("todo: heal read rows: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}
	indexed, err := ix.IX.IndexedDocs(ctx, searchDocKind)
	if err != nil {
		return fmt.Errorf("todo: heal read index: %w", err)
	}
	have := make(map[search.DocRef]struct{}, len(indexed))
	for _, ref := range indexed {
		have[ref] = struct{}{}
	}
	missing := make([]dbgen.ListTodosForIndexRow, 0, len(rows))
	for _, r := range rows {
		if _, ok := have[search.DocRef{Owner: r.Owner, DocID: r.ID}]; !ok {
			missing = append(missing, r)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	// ONE transaction for the batch: a heal that half-committed and then
	// crashed would leave exactly the "some rows missing" state this function
	// exists to fix, and re-running it on the next boot is free (the
	// difference is recomputed). One transaction — rather than one per row —
	// keeps a large install from paying a commit per todo on every boot.
	return dbschema.Transact(ctx, db, "todo search heal", func(ctx context.Context, tx *sql.Tx) error {
		for _, r := range missing {
			if ierr := ix.IndexTodo(ctx, tx, r.Owner, r.ID, r.Title); ierr != nil {
				return ierr
			}
		}
		return nil
	})
}
