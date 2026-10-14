# DB concurrency: single-writer today, MVCC if ever (spike doc)

This is the runbook for a future agent/LLM deciding whether to enable
Turso concurrent writes — read it before touching anything. Status as of
2026-10: writers serialize (single-writer + `busy_timeout`), proven
sufficient; the engine capability below is verified reachable on the pin
but deliberately unadopted.

## Current stance (do not change without measured contention)

- One writer at a time, `?_busy_timeout=10000` in the DSN, short
  transactions. `internal/db/db.go` owns the stance.
- Multi-statement units go through `dbschema.Transact` (begin/commit/
  rollback discipline + busy classification + contention logging);
  single-statement writes stay autocommit (wrapping those would be a
  pass-through layer).
- Contention signal today: `WARN db transact contention` logs (busy at
  commit) and `DEBUG db transact slow` past 1s (invisible waits). No busy
  in the logs = no problem to solve.

## Enabling MVCC (the actual steps, all probed)

1. **One-way file migration.** `PRAGMA journal_mode='mvcc'` persists in
   the file header (Turso ≥0.4): fresh files get it at creation, existing
   production files need an offline migration with **no per-file rollback**.
   WAL alone does NOT enable it (probed: still rejected).
2. **Discipline change.** Every write txn must run `BEGIN CONCURRENT`
   explicitly — and NOT inside `database/sql` `Begin()` (which opens
   DEFERRED and cannot upgrade; probed failure). Pin a connection
   (`db.Conn`) and exec `BEGIN CONCURRENT` first on it.
3. **Conflict-retry wrapper.** Concurrent conflicts ERROR instead of
   blocking: wrap the commit path with bounded retry on
   `dbschema.IsBusyError` (textual classification, table-tested), replay
   only side-effect-free work, keep broadcasts/notifications AFTER the
   successful commit (never inside the retried unit). Idempotency keys
   (`idem_key`, partial today) must cover every retried write first.
4. **Red-proof under contention.** Reproduce the probe (two pinned
   writers, disjoint rows, both commit) plus an overlapping-rows case
   asserting clean conflict errors (not hangs, not partial commits);
   invert each guard (drop the pragma, drop the retry) and watch it fail.

## Revisit triggers

Sustained `db transact contention` WARNs in production logs, or a profile
showing writer queueing — never "the engine supports it" alone. Also
recheck `stack-tradeoffs.md#database-turso...` (engine roadmap, Supabase
stewardship) before investing: the payoff must beat the migration +
retry-discipline cost above.
