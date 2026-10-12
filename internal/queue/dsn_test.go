// SCOPE:core — tests for the Turso DSN configuration.
package queue

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "turso.tech/database/tursogo"
)

// tursoQueueDSN is the DSN Queue.New opens. busy_timeout must live in the
// DSN (a PRAGMA statement is not applied by the driver on open), matching
// the app DB's DSN in internal/db.
func tursoQueueDSN(path string) string {
	return path + "?_busy_timeout=10000"
}

// TestGoqiteDSN_ConcurrentWritersDoNotFail proves the practical effect: two
// connections writing to the same Turso file do not fail with a busy error.
// The second writer must wait for the lock (busy_timeout) and succeed.
// Without the DSN option it would error immediately.
func TestGoqiteDSN_ConcurrentWritersDoNotFail(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "q.db")
	dsn := tursoQueueDSN(path)

	open := func() *sql.DB {
		db, err := sql.Open("turso", dsn)
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
	case <-time.After(15 * time.Second):
		t.Fatal("second writer never completed")
	}
}
