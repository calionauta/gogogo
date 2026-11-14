// The declared outcome, executed against the schema it names.
//
// `recipe check` can only read the query text; a column renamed in the migration
// would keep the text valid and make the ratio quietly return zero for both
// halves. This test applies the recipe's own DDL and runs the two counts the
// declaration carries, so the SQL and the table cannot drift apart unnoticed.
//
// It builds no handler: the declaration and the DDL are the whole subject.
package handlers

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/calionauta/gogogo/features/kiosk"
)

// outcomeRatio is the answer the dashboard renders: the two counts, in the
// order the declaration names them.
func TestOutcomeRatioOverItsOwnSchema(t *testing.T) {
	ctx := context.Background()
	dsn := "file:" + filepath.Join(t.TempDir(), "kiosk.db")
	// The DDL references users(id), a core table this recipe does not own, so
	// the fixture pins the ratio instead of pretending to build a user. The
	// pragma rides the DSN so every pooled connection gets it.
	db, err := sql.Open("sqlite3", dsn+"?_pragma=foreign_keys(OFF)")
	if err != nil {
		t.Fatalf("open sqlite3: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	applyKioskMigrations(ctx, t, db)

	// Two orders for the caller, one of them repaired, plus an order that
	// belongs to someone else: without it, a missing `WHERE owner = ?` would
	// still produce 1/2 and the scoping rule would go unproven.
	insertOrder(ctx, t, db, "o1", "u1", 0)
	insertOrder(ctx, t, db, "o2", "u1", 1)
	insertOrder(ctx, t, db, "o3", "u2", 0)

	d := recipeProvider{}.Declaration()
	outcomes := d.Outcomes()
	if len(outcomes) != 1 {
		t.Fatalf("want exactly one declared outcome, got %d", len(outcomes))
	}
	o := outcomes[0]
	for _, tc := range []struct {
		owner   string
		success int
		total   int
	}{
		{"u1", 1, 2},
		{"u2", 1, 1},
		{"nobody", 0, 0},
	} {
		success := countFor(ctx, t, db, o.Success.Query, tc.owner)
		total := countFor(ctx, t, db, o.Total.Query, tc.owner)
		if success != tc.success || total != tc.total {
			t.Fatalf("outcome %s for %q = %d/%d, want %d/%d",
				o.ID, tc.owner, success, total, tc.success, tc.total)
		}
	}

	// The counterbalance reading is a real count too: the same schema answers
	// it, which is what makes it a fact rather than a note.
	if len(o.Counterbalance) != 1 {
		t.Fatalf("want one counterbalance reading, got %d", len(o.Counterbalance))
	}
	if got := countFor(ctx, t, db, o.Counterbalance[0].Query, "u1"); got != 1 {
		t.Errorf("u1 counterbalance = %d, want 1 (the repaired order)", got)
	}
	if got := countFor(ctx, t, db, o.Counterbalance[0].Query, "u2"); got != 0 {
		t.Errorf("u2 counterbalance = %d, want 0", got)
	}
}

// applyKioskMigrations replays the recipe's embedded DDL, the same bytes the
// seam applies at boot.
func applyKioskMigrations(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()
	entries, err := fs.ReadDir(kiosk.Migrations, "migrations")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		raw, err := fs.ReadFile(kiosk.Migrations, "migrations/"+entry.Name())
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		if _, err := db.ExecContext(ctx, string(raw)); err != nil {
			t.Fatalf("apply %s: %v", entry.Name(), err)
		}
	}
}

func insertOrder(ctx context.Context, t *testing.T, db *sql.DB, id, owner string, corrected int) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
		INSERT INTO ext_kiosk_orders (id, owner, drink, size, milk, extras, price_cents, corrected, confirmed_at)
		VALUES (?, ?, 'latte', 'large', '', '', 450, ?, '2026-11-12T00:00:00Z')`,
		id, owner, corrected)
	if err != nil {
		t.Fatalf("insert order %s: %v", id, err)
	}
}

func countFor(ctx context.Context, t *testing.T, db *sql.DB, query, owner string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, query, owner).Scan(&n); err != nil {
		t.Fatalf("count for %q: %v (query: %s)", owner, err, query)
	}
	return n
}
