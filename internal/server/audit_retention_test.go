// SCOPE:core — audit retention window.
package server

import (
	"context"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

func TestPruneSecurityEventsKeepsTheWindow(t *testing.T) {
	sqlDB, err := db.OpenAt(t.TempDir() + "/audit.db")
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("migrate: %v", merr)
	}
	q := dbgen.New(sqlDB)
	ctx := context.Background()
	old := time.Now().UTC().Add(-auditRetention - time.Hour).Format(dbschema.TimeFormat)
	fresh := dbschema.Now()
	for _, created := range []string{old, fresh} {
		if ierr := q.InsertSecurityEvent(ctx, dbgen.InsertSecurityEventParams{
			ID: dbschema.NewID(), Event: "test", Created: created,
		}); ierr != nil {
			t.Fatalf("insert: %v", ierr)
		}
	}
	pruneSecurityEvents(ctx, q)
	rows, err := q.ListSecurityEvents(ctx, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows after prune = %d, want 1: %+v", len(rows), rows)
	}
	if rows[0].Created != fresh {
		t.Fatalf("kept %q, want the fresh row %q", rows[0].Created, fresh)
	}
}
