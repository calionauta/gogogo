# Debugging with DuckDB

Stdout is JSONL by construction (every emitter goes through the JSON
slog default — see `internal/observability/log.go`), and the Turso file
is plain SQLite. DuckDB reads both without a server, so ad-hoc
questions that grep answers badly ("which requests were slow for
viewer logins?", "how many shared todos exist?") become SQL — with the
`duckdb -ui` browser when you want to click around.

Install once (local machine only, never a dependency):

```bash
brew install duckdb   # or: curl -fsSL https://xerj.org/get | sh
```

## Logs: filter, then query

Stdout carries JSON lines (stderr stays for crashes). Third-party
libraries may add their own lines — filter to JSON first:

```bash
# prod, over SSH; locally, redirect stdout the same way
docker logs gogogo 2>&1 | grep '^{' > /tmp/app.jsonl
duckdb -c "SELECT level, count(*) FROM read_json('/tmp/app.jsonl') GROUP BY level;"
```

Useful shapes:

```sql
-- slow operations first
SELECT msg, dur_ms FROM read_json('/tmp/app.jsonl')
WHERE dur_ms > 500 ORDER BY dur_ms DESC;
-- errors with context
SELECT time, msg, error FROM read_json('/tmp/app.jsonl')
WHERE level = 'ERROR';
```

## Database: checkpoint, copy, attach read-only

The live file has a WAL beside it whose frames an external reader
cannot see — querying the live path returns stale rows. Checkpoint,
copy, and attach the copy read-only. Never point anything at the
running database file.

```bash
# on the box holding the database (prod via SSH, or your data dir)
sqlite3 /path/to/app.db "PRAGMA wal_checkpoint(TRUNCATE);"
cp /path/to/app.db /tmp/dbg.db
duckdb -c "LOAD sqlite; ATTACH '/tmp/dbg.db' AS app (TYPE sqlite, READ_ONLY);
  SELECT visibility, count(*) FROM app.todos GROUP BY visibility;"
```

## Visual: duckdb -ui

```bash
duckdb -ui
```

Then run the same statements: `LOAD sqlite`, `ATTACH ... READ_ONLY`,
`read_json(...)`. Joins across the log view and the tables (e.g.
request durations against row counts) are ordinary SQL.

## Boundaries

- Read-only, local, ad-hoc: no DuckDB in the repo, no daemon in
  production, no committed artifacts (indexes stay out of the tree).
- Secrets never leave their store: query shapes and counts, not
  tokens — `sessions` holds hashes, but prefer not opening it at all.
