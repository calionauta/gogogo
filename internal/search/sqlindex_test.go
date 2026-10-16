// SCOPE:plugin — the SQL index against a real database.
package search

import (
	"context"
	"database/sql"
	"testing"

	"github.com/calionauta/gogogo/internal/db"
	"github.com/calionauta/gogogo/internal/dbschema"
)

func openSearchDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/search.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := dbschema.Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return sqlDB
}

// seed indexes one owner's three titles inside a transaction, the way a
// consumer's write path does.
func seed(t *testing.T, ix SQLIndex, sqlDB *sql.DB, owner string, titles map[string]string) {
	t.Helper()
	ctx := context.Background()
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("tx: %v", err)
	}
	for id, title := range titles {
		if err := ix.IndexDoc(ctx, tx, owner, "todo", id, "title", title); err != nil {
			t.Fatalf("index %s: %v", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}
