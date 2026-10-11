package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// openTestDB is the shared fixture: a migrated database on a temp file.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := OpenAt(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := Migrate(sqlDB); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return sqlDB
}

func insertUser(t *testing.T, sqlDB *sql.DB, email string) string {
	t.Helper()
	id := NewID()
	now := Now()
	_, err := sqlDB.ExecContext(context.Background(),
		`INSERT INTO users (id, email, password_hash, name, created, updated) VALUES (?, ?, ?, ?, ?, ?)`,
		id, email, "hash", "Demo", now, now,
	)
	if err != nil {
		t.Fatalf("insert user %s: %v", email, err)
	}
	return id
}

// TestMigrateIsIdempotent proves the ledger works: a second boot must not
// replay the DDL (which would fail on CREATE TABLE).
func TestMigrateIsIdempotent(t *testing.T) {
	sqlDB := openTestDB(t)
	if err := Migrate(sqlDB); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	var versions []string
	rows, err := sqlDB.QueryContext(context.Background(), `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatalf("list migrations: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan version: %v", err)
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(versions) == 0 || versions[0] != "0001_init" {
		t.Fatalf("schema_migrations = %v, want ledger starting at 0001_init", versions)
	}
}

func TestInsertAndListTodos(t *testing.T) {
	sqlDB := openTestDB(t)
	owner := insertUser(t, sqlDB, "demo1@demo.app")

	now := Now()
	for _, title := range []string{"first", "second"} {
		if _, err := sqlDB.ExecContext(context.Background(),
			`INSERT INTO todos (id, owner, title, completed, idem_key, created, updated) VALUES (?, ?, ?, 0, '', ?, ?)`,
			NewID(), owner, title, now, now,
		); err != nil {
			t.Fatalf("insert todo %q: %v", title, err)
		}
	}

	rows, err := sqlDB.QueryContext(context.Background(),
		`SELECT title FROM todos WHERE owner = ? ORDER BY created DESC`, owner)
	if err != nil {
		t.Fatalf("list todos: %v", err)
	}
	defer rows.Close()

	var titles []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			t.Fatalf("scan todo: %v", err)
		}
		titles = append(titles, title)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate todos: %v", err)
	}
	if len(titles) != 2 {
		t.Fatalf("todos = %v, want 2 rows", titles)
	}
}

// TestForeignKeysEnforced proves PRAGMA foreign_keys actually took effect: an
// orphan todo must be rejected, which is the guarantee the old collection
// relation gave us.
func TestForeignKeysEnforced(t *testing.T) {
	sqlDB := openTestDB(t)
	now := Now()
	_, err := sqlDB.ExecContext(context.Background(),
		`INSERT INTO todos (id, owner, title, completed, idem_key, created, updated) VALUES (?, ?, ?, 0, '', ?, ?)`,
		NewID(), "no-such-user", "orphan", now, now,
	)
	if err == nil {
		t.Fatal("inserting a todo with an unknown owner succeeded; foreign keys are not enforced")
	}
}

// TestIdemKeyPartialUnique proves the dedupe token works and that the partial
// predicate keeps the empty value from colliding — the behaviour the offline
// replay queue depends on.
func TestIdemKeyPartialUnique(t *testing.T) {
	sqlDB := openTestDB(t)
	owner := insertUser(t, sqlDB, "demo1@demo.app")
	now := Now()

	insert := func(key string) error {
		_, err := sqlDB.ExecContext(context.Background(),
			`INSERT INTO todos (id, owner, title, completed, idem_key, created, updated) VALUES (?, ?, ?, 0, ?, ?, ?)`,
			NewID(), owner, "t", key, now, now,
		)
		return err
	}

	if err := insert(""); err != nil {
		t.Fatalf("first empty idem_key: %v", err)
	}
	if err := insert(""); err != nil {
		t.Fatalf("second empty idem_key must not collide: %v", err)
	}
	if err := insert("replay-1"); err != nil {
		t.Fatalf("first replay-1: %v", err)
	}
	if err := insert("replay-1"); err == nil {
		t.Fatal("duplicate idem_key for one owner was accepted")
	}
}

// TestSessionCascade proves deleting a user removes their sessions and todos,
// so a deleted account leaves nothing queryable behind.
func TestSessionCascade(t *testing.T) {
	sqlDB := openTestDB(t)
	owner := insertUser(t, sqlDB, "demo2@demo.app")
	now := Now()

	if _, err := sqlDB.ExecContext(context.Background(),
		`INSERT INTO sessions (token_hash, user_id, expires, created) VALUES (?, ?, ?, ?)`,
		"hash-a", owner, now, now,
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if _, err := sqlDB.ExecContext(context.Background(),
		`INSERT INTO todos (id, owner, title, completed, idem_key, created, updated) VALUES (?, ?, ?, 0, '', ?, ?)`,
		NewID(), owner, "t", now, now,
	); err != nil {
		t.Fatalf("insert todo: %v", err)
	}

	if _, err := sqlDB.ExecContext(context.Background(), `DELETE FROM users WHERE id = ?`, owner); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	for _, q := range []struct {
		name  string
		query string
	}{
		{"sessions", `SELECT count(*) FROM sessions WHERE user_id = ?`},
		{"todos", `SELECT count(*) FROM todos WHERE owner = ?`},
	} {
		var n int
		if err := sqlDB.QueryRowContext(context.Background(), q.query, owner).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", q.name, err)
		}
		if n != 0 {
			t.Errorf("%s left %d rows after user delete, want 0", q.name, n)
		}
	}
}

// TestNewIDIsOrdered pins the property the schema relies on: UUIDv7 sorted as
// text is sorted in time, so `ORDER BY id` is a creation order.
func TestNewIDIsOrdered(t *testing.T) {
	first, second := NewID(), NewID()
	if first >= second {
		t.Fatalf("NewID not time-ordered: %q >= %q", first, second)
	}
}
