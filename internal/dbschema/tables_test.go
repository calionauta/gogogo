// SCOPE:core — the core table set is data, and this is what keeps it honest.
//
// The recipe boundary ("a recipe may never own a core object") is only as good
// as the list it checks against. A hand-maintained list in another package had
// already drifted twice: it named `magic_links` and `config`, which no migration
// creates, and it omitted `notes`, `boards`, `notifications` and `op_receipts`,
// which are real — so a recipe could claim those tables and pass.
//
// The comparison is exact and bidirectional against a migrated database, because
// that is the only source that cannot flatter itself: a new migration that adds a
// table without an ownership statement fails here, and a dropped table cannot
// linger as a phantom boundary.
package dbschema

import (
	"context"
	"slices"
	"testing"
)

func TestCoreTablesMatchTheMigratedSchema(t *testing.T) {
	sqlDB := openTestDB(t)

	rows, err := sqlDB.QueryContext(context.Background(),
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var effective []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		effective = append(effective, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate: %v", err)
	}

	declared := CoreTables()
	for _, name := range effective {
		if !slices.Contains(declared, name) {
			t.Errorf("migration creates table %q that CoreTables does not list: a recipe could claim it", name)
		}
	}
	for _, name := range declared {
		if !slices.Contains(effective, name) {
			t.Errorf("CoreTables lists %q, which no migration creates: the boundary names a table that does not exist", name)
		}
	}
}

// TestCoreTablesIsSortedAndUnique keeps the list diffable and the membership
// check cheap; an unsorted list is also how a duplicate hides.
func TestCoreTablesIsSortedAndUnique(t *testing.T) {
	tables := CoreTables()
	if !slices.IsSorted(tables) {
		t.Fatalf("CoreTables must be sorted, got %v", tables)
	}
	if len(slices.Compact(slices.Clone(tables))) != len(tables) {
		t.Fatalf("CoreTables holds a duplicate, got %v", tables)
	}
}
