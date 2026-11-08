// The todo index writer: the postings half of every mutation.
//
// The transaction implementations live in the domain service
// (features/todo/service). These wrappers keep the bridge + test call
// sites working during the migration; new code should use the service.
package handlers

import (
	"context"
	"database/sql"

	"github.com/calionauta/gogogo/features/todo/service"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
)

func createTodoIndexed(ctx context.Context, q *dbgen.Queries, ix SearchIndexer, db *sql.DB,
	p dbgen.CreateTodoParams,
) error {
	return service.New(q, ix, db, nil, nil).CreateIndexed(ctx, p)
}

func deleteTodoIndexed(ctx context.Context, q *dbgen.Queries, ix SearchIndexer, db *sql.DB,
	owner, id string,
) error {
	return service.New(q, ix, db, nil, nil).DeleteIndexed(ctx, owner, id)
}

func clearCompletedIndexed(ctx context.Context, q *dbgen.Queries, ix SearchIndexer, db *sql.DB,
	owner string,
) (int64, error) {
	return service.New(q, ix, db, nil, nil).ClearCompletedIndexed(ctx, owner)
}
