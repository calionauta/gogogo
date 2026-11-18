package config

import (
	"path/filepath"
	"testing"
)

// TestDataDirMovesEveryStore pins one contract: DATA_DIR is the root, and each
// per-store path defaults UNDER it.
//
// The bug this exists for: TURSO_DB_PATH, NATS_STORE_DIR and DAGNATS_STORE_DIR
// defaulted to the literal "data/…", so moving DATA_DIR moved the queues, the
// credentials and NATS but left the database at ./data/app.db. Two processes
// with different DATA_DIRs then shared one SQLite file — which is how a
// debugging session burned three boots on "failed locking file 'data/app.db'"
// while looking at a completely different data dir. docs/configuration.md
// already claimed the coupling ("under DATA_DIR"); the code did not implement
// it.
func TestDataDirMovesEveryStore(t *testing.T) {
	// Not parallel: t.Setenv forbids it.
	dir := filepath.Join(t.TempDir(), "elsewhere")
	t.Setenv("DATA_DIR", dir)
	// Blank the explicit overrides so the defaults are what is measured.
	for _, k := range []string{"TURSO_DB_PATH", "NATS_STORE_DIR", "DAGNATS_STORE_DIR", "LICENSE_FILE"} {
		t.Setenv(k, "")
	}

	cfg := Load()

	for _, want := range []struct {
		name string
		got  string
		rel  string
	}{
		{"TURSO_DB_PATH", cfg.Turso.DBPath, "app.db"},
		{"NATS_STORE_DIR", cfg.NATS.StoreDir, "nats"},
		{"DAGNATS_STORE_DIR", cfg.DagNats.StoreDir, "dagnats"},
		{"LICENSE_FILE", cfg.LicenseFile, "license.token"},
	} {
		if expected := filepath.Join(dir, want.rel); want.got != expected {
			t.Errorf("%s = %q, want %q (a store outside DATA_DIR is invisible to a backup of DATA_DIR)",
				want.name, want.got, expected)
		}
	}
}

// TestExplicitStorePathBeatsDataDir is the other half: a deploy that pins a
// path must not be moved by DATA_DIR, or the fix would trade a silent shared
// file for a silent relocation.
func TestExplicitStorePathBeatsDataDir(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("TURSO_DB_PATH", "/var/lib/pinned/app.db")
	t.Setenv("NATS_STORE_DIR", "/var/lib/pinned/nats")

	cfg := Load()

	if cfg.Turso.DBPath != "/var/lib/pinned/app.db" {
		t.Errorf("TURSO_DB_PATH = %q, an explicit value must win over DATA_DIR", cfg.Turso.DBPath)
	}
	if cfg.NATS.StoreDir != "/var/lib/pinned/nats" {
		t.Errorf("NATS_STORE_DIR = %q, an explicit value must win over DATA_DIR", cfg.NATS.StoreDir)
	}
}
