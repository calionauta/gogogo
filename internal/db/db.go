// SCOPE:core — application database: the one place that knows the Turso
// Database connection string, file layout, and pragmas. Schema, versions,
// IDs and timestamps live in internal/dbschema (driver-free, shared with
// zero-CGO targets); this package owns the open through a platform driver —
// tursogo on desktop/server, ncruces on Android (tursogo embeds no Android
// library). Handlers never touch
// this package directly: they receive sqlc-generated queries (internal/db/queries)
// built on the *sql.DB returned here.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/calionauta/gogogo/config"
)

// Driver is the database/sql driver name for this platform: tursogo on
// desktop/server, ncruces on Android (tursogo has no Android library).
const Driver = sqlDriver

// busyTimeoutMS is how long a writer waits for a competing writer before
// returning SQLITE_BUSY. Turso serializes writes; 10s matches the previous
// single-writer stance and absorbs the burst of a page render plus a job.
const busyTimeoutMS = 10_000

// dirPerm is the mode for the database parent dir created on first boot.
// Group/other read+execute lets backups and sidecars inspect data/ without
// write access; the database file itself keeps the process umask.
const dirPerm = 0o755

// Open opens the application database from cfg.
//
// An empty cfg.Turso.URL means a single-box deployment: a local file at
// cfg.Turso.DBPath (default data/app.db), created along with its parent
// directory on first boot. A non-empty URL is the remote Turso target, which
// needs the sync driver (db.OpenRemote) and is not wired yet — failing fast
// here beats silently writing to a local file while the operator believes
// they are on a remote database.
//
// The returned handle is a pool. Turso gives readers MVCC snapshots, but
// writers still serialize, so callers keep transactions short rather than
// holding a connection as a lock.
func Open(cfg *config.Config) (*sql.DB, error) {
	if cfg.Turso.URL != "" {
		return nil, fmt.Errorf(
			"db: remote Turso (TURSO_DATABASE_URL=%q) is not wired yet; leave it empty to use the local file at %s",
			cfg.Turso.URL, cfg.Turso.DBPath)
	}
	path := cfg.Turso.DBPath
	if path == "" {
		path = filepath.Join(cfg.DataDir, "app.db")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return nil, fmt.Errorf("db: create data dir %q: %w", dir, err)
		}
	}

	sqlDB, err := sql.Open(Driver, localDSN(path))
	if err != nil {
		return nil, fmt.Errorf("db: open %q: %w", path, err)
	}
	if err := applyPragmas(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return sqlDB, nil
}

// applyPragmas sets the connection-level pragmas the engine does not take as
// DSN options. foreign_keys is per-connection (not persisted in the file), so
// it must run on every pooled connection; a failure here is fatal because a
// silent FK-off database accepts orphans the schema promises to prevent.
func applyPragmas(sqlDB *sql.DB) error {
	if _, err := sqlDB.ExecContext(context.Background(), "PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("db: enable foreign_keys: %w", err)
	}
	return nil
}

// OpenAt is the test/CLI entry point: it opens a database at an explicit path
// with no config in the way. Production code goes through Open so the file
// layout stays in one place.
func OpenAt(path string) (*sql.DB, error) {
	cfg := &config.Config{}
	cfg.Turso.DBPath = path
	return Open(cfg)
}
