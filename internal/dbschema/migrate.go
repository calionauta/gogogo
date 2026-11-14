// Driver-free schema + identity: embedded migrations,
// versions ledger, IDs and timestamps. No driver import here on purpose:
// native targets (cmd/gui, zero CGO) open via ncruces `sqlite3` and share
// this package, while internal/db owns the tursogo open. Importing
// internal/db links tursogo (purego) — importing this package must not.
//
// Migrations are plain SQL under migrations/, applied by Migrate on every
// boot and recorded in schema_migrations. There is no down-migration and no
// separate tool: the files ARE the schema, so `git diff` shows the whole
// history of the data model, and a fresh clone builds the same database as a
// deployed one.
package dbschema

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// coreMigrations is the embedded set. Its versions stay unprefixed, so an
// existing database upgrades exactly as it always did.
var coreMigrations = migrationSource{label: "core", fsys: migrationsFS, dir: "migrations"}

// MigrationSet is one filesystem of migrations applied under a namespace.
//
// A recipe passes its own FS and its recipe id; its 0001_init.sql lands in the
// ledger as "<id>/0001_init", and its table names carry the recipe prefix
// (recipe.Validate enforces that half). Core never builds one of these: its
// versions are unprefixed by design.
type MigrationSet struct {
	// ID namespaces the versions. Empty is reserved for the embedded core set.
	ID string
	// FS holds the .sql files.
	FS fs.FS
	// Dir is the directory inside FS; empty means its root.
	Dir string
}

// Migrate applies every embedded migration that is not yet recorded in
// schema_migrations, in filename order. Each file is applied inside one
// transaction together with its version row, so a failure leaves neither a
// half-applied schema nor a version claiming work that did not happen.
//
// Calling it on every boot is intended: the applied set is read first, so the
// steady state is one SELECT.
func Migrate(sqlDB *sql.DB) error {
	return MigrateWith(sqlDB)
}

// MigrateWith applies the embedded core set and then every recipe set, each
// under its own version namespace ("kiosk/0001_init"). Order is core first,
// then sets sorted by ID: a boot is deterministic, and a recipe may reference a
// core table.
//
// Idempotence holds per filesystem. The ledger's primary key is the prefixed
// version, so two recipes may both number their files from 0001.
func MigrateWith(sqlDB *sql.DB, extra ...MigrationSet) error {
	if err := ensureMigrationsTable(sqlDB); err != nil {
		return err
	}
	applied, err := appliedVersions(sqlDB)
	if err != nil {
		return err
	}
	sources, err := migrationSources(extra)
	if err != nil {
		return err
	}
	n := 0
	for _, src := range sources {
		versions, verr := src.versions()
		if verr != nil {
			return verr
		}
		for _, v := range versions {
			key := src.key(v)
			if applied[key] {
				continue
			}
			if err := applyMigration(sqlDB, src, v, key); err != nil {
				return err
			}
			applied[key] = true
			n++
		}
	}
	if n > 0 {
		slog.Info("dbschema: migrations applied", "count", n)
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
		return fmt.Errorf("dbschema: create schema_migrations: %w", err)
	}
	return nil
}

// appliedVersions reads the ledger into a set.
func appliedVersions(sqlDB *sql.DB) (map[string]bool, error) {
	rows, err := sqlDB.QueryContext(context.Background(), `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("dbschema: read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("dbschema: scan schema_migrations: %w", err)
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dbschema: iterate schema_migrations: %w", err)
	}
	return applied, nil
}

// migrationSource is one filesystem of migrations plus the namespace its
// versions are recorded under.
type migrationSource struct {
	label  string // log and error text
	prefix string // "" for core, "<id>/" otherwise
	fsys   fs.FS
	dir    string
}

// key is the ledger version for one file: the namespace plus the file name.
func (s migrationSource) key(version string) string { return s.prefix + version }

// root is the directory ReadDir needs: "." when the FS is rooted at its own
// migrations, because io/fs refuses an empty name.
func (s migrationSource) root() string {
	if s.dir == "" {
		return "."
	}
	return s.dir
}

// versions lists the .sql files as sorted versions ("0001_init").
func (s migrationSource) versions() ([]string, error) {
	entries, err := fs.ReadDir(s.fsys, s.root())
	if err != nil {
		return nil, fmt.Errorf("dbschema: read %s migrations: %w", s.label, err)
	}
	versions := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		versions = append(versions, strings.TrimSuffix(name, ".sql"))
	}
	sort.Strings(versions)
	return versions, nil
}

// migrationSources returns the core set followed by the recipe sets sorted by
// ID, refusing one that cannot be namespaced: an empty id would collide with
// core, and two sets sharing an id would collide with each other.
func migrationSources(extra []MigrationSet) ([]migrationSource, error) {
	out := make([]migrationSource, 0, 1+len(extra))
	out = append(out, coreMigrations)
	seen := make(map[string]bool, len(extra))
	for _, set := range extra {
		if set.ID == "" {
			return nil, errors.New("dbschema: migration set needs an id (only core is unprefixed)")
		}
		if strings.Contains(set.ID, "/") {
			return nil, fmt.Errorf("dbschema: migration set id %q must not contain a slash", set.ID)
		}
		if seen[set.ID] {
			return nil, fmt.Errorf("dbschema: migration set %q declared twice", set.ID)
		}
		seen[set.ID] = true
		out = append(out, migrationSource{
			label:  set.ID,
			prefix: set.ID + "/",
			fsys:   set.FS,
			dir:    set.Dir,
		})
	}
	sort.Slice(out[1:], func(i, j int) bool { return out[1+i].label < out[1+j].label })
	return out, nil
}

// migrationVersions lists the embedded files as sorted versions ("0001_init").
func migrationVersions() ([]string, error) {
	return coreMigrations.versions()
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
func applyMigration(sqlDB *sql.DB, src migrationSource, version, key string) error {
	sqlBytes, err := fs.ReadFile(src.fsys, path.Join(src.dir, version+".sql"))
	if err != nil {
		return fmt.Errorf("dbschema: read migration %s: %w", key, err)
	}
	return Transact(context.Background(), sqlDB, "migrate "+key, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, string(sqlBytes)); err != nil {
			return fmt.Errorf("dbschema: apply migration %s: %w", key, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, key, Now()); err != nil {
			return fmt.Errorf("dbschema: record migration %s: %w", key, err)
		}
		return nil
	})
}
