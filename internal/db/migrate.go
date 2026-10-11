// SCOPE:core — schema migrations: embedded .sql files applied in order.
//
// Migrations are plain SQL under migrations/, applied by Migrate on every
// boot and recorded in schema_migrations. There is no down-migration and no
// separate tool: the files ARE the schema, so `git diff` shows the whole
// history of the data model, and a fresh clone builds the same database as a
// deployed one.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies every embedded migration that is not yet recorded in
// schema_migrations, in filename order. Each file is applied inside one
// transaction together with its version row, so a failure leaves neither a
// half-applied schema nor a version claiming work that did not happen.
//
// Calling it on every boot is intended: the applied set is read first, so the
// steady state is one SELECT.
func Migrate(sqlDB *sql.DB) error {
	if err := ensureMigrationsTable(sqlDB); err != nil {
		return err
	}
	applied, err := appliedVersions(sqlDB)
	if err != nil {
		return err
	}
	versions, err := migrationVersions()
	if err != nil {
		return err
	}
	for _, v := range versions {
		if applied[v] {
			continue
		}
		if err := applyMigration(sqlDB, v); err != nil {
			return err
		}
	}
	return nil
}

// ensureMigrationsTable creates the ledger. It is deliberately not a
// migration itself: the ledger must exist before the first migration runs.
func ensureMigrationsTable(sqlDB *sql.DB) error {
	_, err := sqlDB.ExecContext(context.Background(), `CREATE TABLE IF NOT EXISTS schema_migrations (
    version    TEXT PRIMARY KEY,
    applied_at TEXT NOT NULL
)`)
	if err != nil {
		return fmt.Errorf("db: create schema_migrations: %w", err)
	}
	return nil
}

// appliedVersions reads the ledger into a set.
func appliedVersions(sqlDB *sql.DB) (map[string]bool, error) {
	rows, err := sqlDB.QueryContext(context.Background(), `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("db: read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("db: scan schema_migrations: %w", err)
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: iterate schema_migrations: %w", err)
	}
	return applied, nil
}

// migrationVersions lists the embedded files as sorted versions ("0001_init").
func migrationVersions() ([]string, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("db: read migrations dir: %w", err)
	}
	versions := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		versions = append(versions, strings.TrimSuffix(e.Name(), ".sql"))
	}
	sort.Strings(versions)
	return versions, nil
}

// Versions exposes the embedded migration list for tooling and tests: the
// applied ledger must match this exactly (no missing, no extras).
func Versions() ([]string, error) {
	return migrationVersions()
}

// applyMigration runs one file and records it, both inside one transaction.
// The whole file is sent as a single Exec: tursogo executes every statement in
// the string, which is why the migrations use semicolon-terminated statements
// and avoid semicolons inside literals.
func applyMigration(sqlDB *sql.DB, version string) error {
	sqlBytes, err := migrationsFS.ReadFile("migrations/" + version + ".sql")
	if err != nil {
		return fmt.Errorf("db: read migration %s: %w", version, err)
	}
	tx, err := sqlDB.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("db: begin migration %s: %w", version, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(context.Background(), string(sqlBytes)); err != nil {
		return fmt.Errorf("db: apply migration %s: %w", version, err)
	}
	if _, err := tx.ExecContext(context.Background(),
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, version, Now()); err != nil {
		return fmt.Errorf("db: record migration %s: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: commit migration %s: %w", version, err)
	}
	return nil
}
