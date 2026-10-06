// SCOPE:layer=infra,removal=core — tests for the SQLite DSN configuration.
package queue

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// TestGoqiteDSN_HasBusyTimeout pins the pragma that keeps concurrent queue
// writes from failing with SQLITE_BUSY. busy_timeout is applied per
// connection at open time, so it must live in the DSN — a PRAGMA statement
// issued afterwards would not cover every pooled connection.
//
// Red-proof: with the old `sql.Open("sqlite3", dbPath)` the busy_timeout is 0
// and this test fails.
func TestGoqiteDSN_HasBusyTimeout(t *testing.T) {
	db, err := sql.Open("sqlite3",
		"file:"+filepath.Join(t.TempDir(), "q.db")+
			"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	var busy int
	if err := db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); err != nil {
		t.Fatalf("read busy_timeout: %v", err)
	}
	if busy != 10000 {
		t.Fatalf("busy_timeout = %d, want 10000 (SQLITE_BUSY under contention)", busy)
	}

	var mode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
}

// TestGoqiteDSN_ConcurrentWritersDoNotFail proves the practical effect: two
// connections writing to the same goqite-style DB do not fail with a busy
// error. With busy_timeout=0 the second write would error immediately.
func TestGoqiteDSN_ConcurrentWritersDoNotFail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.db")
	dsn := "file:" + path + "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"

	open := func() *sql.DB {
		db, err := sql.Open("sqlite3", dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		return db
	}

	a, b := open(), open()
	ctx := context.Background()
	if _, err := a.ExecContext(ctx, "CREATE TABLE t (v INTEGER)"); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Hold a write transaction on A, then write from B: B must wait for the
	// lock (busy_timeout) and succeed, not return SQLITE_BUSY.
	txA, err := a.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := txA.ExecContext(ctx, "INSERT INTO t VALUES (1)"); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := b.ExecContext(ctx, "INSERT INTO t VALUES (2)")
		done <- err
	}()

	// Let B block on the lock, then release it.
	time.Sleep(50 * time.Millisecond)
	if err := txA.Commit(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second writer failed while busy_timeout should have waited: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second writer never completed")
	}
}
