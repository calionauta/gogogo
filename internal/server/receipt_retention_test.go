// SCOPE:plugin — op-receipt retention window.
package server

import (
	"context"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// TestPruneOpReceiptsKeepsTheReplayWindow proves the retention policy that
// makes the receipt safe: a receipt must outlive the outbox entry it protects.
// The window is checked from both sides — an expired receipt is deleted, a
// fresh one stays — because deleting too eagerly is the failure that silently
// re-enables the double-apply bug.
func TestPruneOpReceiptsKeepsTheReplayWindow(t *testing.T) {
	sqlDB, err := db.OpenAt(t.TempDir() + "/receipts.db")
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("migrate: %v", merr)
	}
	q := dbgen.New(sqlDB)
	ctx := context.Background()

	owner := dbschema.NewID()
	if _, uerr := q.CreateUser(ctx, dbgen.CreateUserParams{
		ID: owner, Email: "retention@example.com", Created: dbschema.Now(), Updated: dbschema.Now(),
	}); uerr != nil {
		t.Fatalf("user: %v", uerr)
	}

	// Literal ages, NOT offsets from opReceiptRetention: the requirement is
	// "a receipt outlives the outbox entry it protects", and the browser drops
	// a queued op after a week. A six-day-old receipt MUST survive; a
	// fifteen-day-old one must not. Written against the constant, this test
	// would pass even if the window shrank to an hour — a tautology.
	const outboxWeek = 7 * 24 * time.Hour
	expired := time.Now().UTC().Add(-15 * 24 * time.Hour).Format(dbschema.TimeFormat)
	fresh := time.Now().UTC().Add(-outboxWeek + 24*time.Hour).Format(dbschema.TimeFormat)
	if outboxWeek >= opReceiptRetention {
		t.Fatalf("opReceiptRetention (%v) must exceed the outbox TTL (%v)", opReceiptRetention, outboxWeek)
	}
	for i, created := range []string{expired, fresh} {
		claimed, cerr := q.ClaimOpReceipt(ctx, dbgen.ClaimOpReceiptParams{
			Owner: owner, OpKey: "op-" + string(rune('a'+i)), Created: created,
		})
		if cerr != nil || claimed != 1 {
			t.Fatalf("claim %d = %d, %v", i, claimed, cerr)
		}
	}

	pruneOpReceipts(ctx, q)

	for i, want := range []bool{false, true} {
		claimed, cerr := q.ClaimOpReceipt(ctx, dbgen.ClaimOpReceiptParams{
			Owner: owner, OpKey: "op-" + string(rune('a'+i)), Created: dbschema.Now(),
		})
		if cerr != nil {
			t.Fatalf("re-claim %d: %v", i, cerr)
		}
		// claimed=1 means the row is gone (the insert succeeded); claimed=0
		// means it survived the prune.
		if got := claimed == 0; got != want {
			t.Errorf("receipt %d survived prune = %v, want %v", i, got, want)
		}
	}
}
