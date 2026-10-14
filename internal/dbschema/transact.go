// SCOPE:core — single home for multi-statement transactions: begin,
// commit/rollback discipline, busy classification and contention logging.
// Driver-free (stdlib only): this package is linked by zero-CGO native
// targets, so nothing here may import tursogo (see dbschema_test.go).
package dbschema

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// slowTxnLogThreshold is the elapsed time past which a committed
// transaction logs at DEBUG with its duration. A busy_timeout wait is
// invisible until it errors (10s by default), so slow-but-successful
// commits are the contention signal — the only cheap one a
// driver-agnostic helper can observe.
const slowTxnLogThreshold = time.Second

// Transact runs fn inside a single transaction with the given op name
// (used in logs only): begin with default options, roll back when fn
// fails, commit otherwise. Commit failures classified as lock contention
// log at WARN with the operation and elapsed time; anything slower than
// slowTxnLogThreshold logs at DEBUG even on success. Single-statement
// writes do NOT need this (autocommit + DSN busy_timeout already cover
// them — wrapping those would be a pass-through layer); use it for real
// multi-statement units (migrations today, MVCC conflict-retry tomorrow:
// docs/db-concurrency.md).
func Transact(ctx context.Context, sqlDB *sql.DB, op string, fn func(context.Context, *sql.Tx) error) error {
	start := time.Now()
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db transact %s begin: %w", op, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := fn(ctx, tx); err != nil {
		return fmt.Errorf("db transact %s: %w", op, err)
	}
	if err := tx.Commit(); err != nil {
		elapsed := time.Since(start)
		if IsBusyError(err) {
			slog.WarnContext(ctx, "db transact contention",
				"op", op, "elapsed", elapsed, "err", err)
		}
		return fmt.Errorf("db transact %s commit: %w", op, err)
	}
	if elapsed := time.Since(start); elapsed >= slowTxnLogThreshold {
		slog.DebugContext(ctx, "db transact slow", "op", op, "elapsed", elapsed)
	}
	committed = true
	return nil
}

// IsBusyError reports whether err looks like SQLite lock contention
// ("database is locked" family). Best-effort textual classification —
// drivers wrap the core error opaquely, so there is no error code to
// switch on; the strings below are the stable SQLite wordings both
// tursogo and ncruces surface. Callers use it to decide retry-vs-fail
// (notably a future MVCC conflict-retry wrapper, whose conflicts must
// retry while anything else must fail).
func IsBusyError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, sub := range []string{
		"database is locked",
		"database table is locked",
		"database schema is locked",
		"sqlite_busy",
	} {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
