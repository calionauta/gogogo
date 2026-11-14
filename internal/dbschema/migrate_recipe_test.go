package dbschema

import (
	"context"
	"testing"
	"testing/fstest"
)

// TestRecipeMigrationReachesLedger is phase 1.2 of
// docs/research/extensibility-2026-11.md: a recipe ships SQL with the code it
// copies, and that SQL has to reach the same ledger the core schema uses.
//
// The two sets below deliberately share the version NAME (0001_init), which is
// what any two recipes numbering their own files from 0001 will do. The ledger's
// primary key is the version string, so the namespace is what keeps them apart:
// without it the second recipe fails on a duplicate key, and the failure lands
// on whichever one the user installed second.
func TestRecipeMigrationReachesLedger(t *testing.T) {
	sqlDB := openTestDB(t)
	counter := fstest.MapFS{
		"0001_init.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE ext_counter_runs (id TEXT PRIMARY KEY);"),
		},
	}
	timer := fstest.MapFS{
		"0001_init.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE ext_timer_runs (id TEXT PRIMARY KEY);"),
		},
	}
	sets := []MigrationSet{
		{ID: "counter", FS: counter},
		{ID: "timer", FS: timer},
	}
	if err := MigrateWith(sqlDB, sets...); err != nil {
		t.Fatalf("MigrateWith: %v", err)
	}
	for _, want := range []string{"counter/0001_init", "timer/0001_init"} {
		var got string
		row := sqlDB.QueryRowContext(context.Background(),
			`SELECT version FROM schema_migrations WHERE version = ?`, want)
		if err := row.Scan(&got); err != nil {
			t.Errorf("recipe version %q missing from schema_migrations: %v", want, err)
		}
	}
	for _, table := range []string{"ext_counter_runs", "ext_timer_runs"} {
		var name string
		row := sqlDB.QueryRowContext(context.Background(),
			`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`, table)
		if err := row.Scan(&name); err != nil {
			t.Errorf("recipe table %q was not created: %v", table, err)
		}
	}
	// Core keeps its own namespace: the ledger carries the embedded versions
	// unprefixed, so an existing database upgrades exactly as before.
	core, err := Versions()
	if err != nil || len(core) == 0 {
		t.Fatalf("core versions: %v (%d)", err, len(core))
	}
	var got string
	coreRow := sqlDB.QueryRowContext(context.Background(),
		`SELECT version FROM schema_migrations WHERE version = ?`, core[0])
	if err := coreRow.Scan(&got); err != nil {
		t.Errorf("core version %q missing from schema_migrations: %v", core[0], err)
	}

	// Idempotence holds PER filesystem: the steady state is a read, and a
	// recipe that already ran must not re-run because its sibling moved.
	if err := MigrateWith(sqlDB, sets...); err != nil {
		t.Fatalf("second MigrateWith: %v", err)
	}
}
