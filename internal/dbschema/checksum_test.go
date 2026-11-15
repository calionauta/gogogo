// Append-only is a fact, not a convention.
//
// The docs promise a migration file is history: it runs once and never changes.
// Nothing enforced that before — the ledger recorded only a version and a
// timestamp — so editing an applied 0007 would reach every NEW database and
// never reach the deployed one, leaving two different schemas with every gate
// green.
package dbschema

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"testing/fstest"
)

// migrateOnce applies a set and returns the database, so a test can then change
// what the filesystem holds.
func migrateOnce(t *testing.T, fsys fstest.MapFS) *sql.DB {
	t.Helper()
	db := openTestDB(t)
	if err := MigrateWith(db, MigrationSet{ID: "probe", FS: fsys}); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	return db
}

// TestEditingAnAppliedMigrationIsRefused pins the one rule an append-only
// ledger can actually enforce.
func TestEditingAnAppliedMigrationIsRefused(t *testing.T) {
	fsys := fstest.MapFS{
		"0001_init.sql": &fstest.MapFile{Data: []byte("CREATE TABLE probe_a (id TEXT PRIMARY KEY);")},
	}
	h := migrateOnce(t, fsys)

	// The statement changes but the name does not: exactly what an edit looks
	// like. A fresh database would create probe_b; the applied one never will.
	fsys["0001_init.sql"] = &fstest.MapFile{
		Data: []byte("CREATE TABLE probe_b (id TEXT PRIMARY KEY);"),
	}
	err := MigrateWith(h, MigrationSet{ID: "probe", FS: fsys})
	if err == nil {
		t.Fatal("an edited migration was re-accepted")
	}
	if !strings.Contains(err.Error(), "append-only") {
		t.Errorf("the refusal does not say what the rule is: %v", err)
	}
}

// TestAnUnchangedMigrationIsNotReapplied is the other half: the steady state
// must stay a no-op, or every boot would fail.
func TestAnUnchangedMigrationIsNotReapplied(t *testing.T) {
	fsys := fstest.MapFS{
		"0001_init.sql": &fstest.MapFile{Data: []byte("CREATE TABLE probe_ok (id TEXT PRIMARY KEY);")},
	}
	h := migrateOnce(t, fsys)
	if err := MigrateWith(h, MigrationSet{ID: "probe", FS: fsys}); err != nil {
		t.Fatalf("a second boot failed: %v", err)
	}
}

// TestALedgerRowWithoutAChecksumIsAdopted: a database created before the column
// existed must keep working, and the next boot records the digest instead of
// treating the missing value as a change.
func TestALedgerRowWithoutAChecksumIsAdopted(t *testing.T) {
	fsys := fstest.MapFS{
		"0001_init.sql": &fstest.MapFile{Data: []byte("CREATE TABLE probe_adopt (id TEXT PRIMARY KEY);")},
	}
	h := migrateOnce(t, fsys)
	if _, err := h.ExecContext(context.Background(),
		`UPDATE schema_migrations SET checksum = NULL WHERE version = 'probe/0001_init'`); err != nil {
		t.Fatalf("clear checksum: %v", err)
	}

	if err := MigrateWith(h, MigrationSet{ID: "probe", FS: fsys}); err != nil {
		t.Fatalf("adoption failed: %v", err)
	}
	var got string
	if err := h.QueryRowContext(context.Background(),
		`SELECT COALESCE(checksum, '') FROM schema_migrations WHERE version = 'probe/0001_init'`).
		Scan(&got); err != nil {
		t.Fatalf("read checksum: %v", err)
	}
	if got == "" {
		t.Error("the digest was not adopted on the next boot")
	}
}
