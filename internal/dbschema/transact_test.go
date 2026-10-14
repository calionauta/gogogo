package dbschema

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
)

func openTransactTestDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "transact.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	const schema = `CREATE TABLE t (id INTEGER PRIMARY KEY, v INTEGER)`
	if _, err := sqlDB.ExecContext(context.Background(), schema); err != nil {
		t.Fatalf("create: %v", err)
	}
	return sqlDB
}

func TestTransactCommits(t *testing.T) {
	t.Parallel()
	sqlDB := openTransactTestDB(t)
	err := Transact(context.Background(), sqlDB, "test-commit", func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO t (v) VALUES (1), (2)`)
		return err
	})
	if err != nil {
		t.Fatalf("Transact: %v", err)
	}
	var n int
	if err := sqlDB.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM t`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("rows = %d, want 2", n)
	}
}

func TestTransactRollsBackOnFnError(t *testing.T) {
	t.Parallel()
	sqlDB := openTransactTestDB(t)
	boom := errors.New("boom")
	err := Transact(context.Background(), sqlDB, "test-rollback", func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO t (v) VALUES (1)`); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want wrapped boom", err)
	}
	var n int
	if err := sqlDB.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM t`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("rows = %d after fn error, want 0 (rollback missing)", n)
	}
	// A mere abandoned transaction would also read 0 from here while still
	// holding the write lock — so prove release, not just invisibility: a
	// follow-up write must succeed immediately instead of hitting busy.
	if err := Transact(context.Background(), sqlDB, "test-release", func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO t (v) VALUES (2)`)
		return err
	}); err != nil {
		t.Fatalf("follow-up write after rollback: %v (lock still held?)", err)
	}
}

func TestIsBusyError(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"locked", errors.New("database is locked"), true},
		{"table locked", errors.New("database table is locked"), true},
		{"schema locked", errors.New("database schema is locked"), true},
		{"sqlite busy code", errors.New("SQLITE_BUSY: database is locked"), true},
		{"wrapped", errors.New("commit: database is locked"), true},
		{"constraint", errors.New("UNIQUE constraint failed: t.id"), false},
		{"syntax", errors.New("near \"FROM\": syntax error"), false},
		{"business as usual", errors.New("business as usual"), false},
	} {
		if got := IsBusyError(tc.err); got != tc.want {
			t.Errorf("%s: IsBusyError = %v, want %v", tc.name, got, tc.want)
		}
	}
}
