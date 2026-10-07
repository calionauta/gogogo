#!/usr/bin/env bash
# backup.sh — crash-consistent backup of every server-side store.
#
# What it covers: data.db + queue.db via sqlite3 .backup (transactionally
# safe — never cp a live SQLite file), JetStream via `nats account backup`
# when the nats CLI can reach the server, then one tarball. Every store
# here is crash-recoverable by design (SQLite WAL replay, JetStream
# recovery, goqite), so a point-in-time file set restores cleanly.
# What it does NOT cover: browser state (IndexedDB outbox, localStorage).
#
# Usage: ./scripts/backup.sh [DATA_DIR] [DEST_DIR]
#   DATA_DIR defaults to ./data, DEST_DIR to ./backups/gogogo-<timestamp>.
# Degrades loudly: missing sqlite3 CLI is fatal (nothing to back up
# with); missing/unreachable nats CLI skips JetStream with a warning.
set -euo pipefail

DATA_DIR="${1:-./data}"
STAMP="$(date +%Y%m%d-%H%M%S)"
DEST="${2:-./backups/gogogo-$STAMP}"
mkdir -p "$DEST"

command -v sqlite3 >/dev/null || { echo "backup: sqlite3 CLI not found" >&2; exit 1; }

sqlite_backup() { # $1 = db file, $2 = dest file
	if [ ! -f "$1" ]; then
		echo "backup: skip $1 (absent — fresh install?)"
		return 0
	fi
	sqlite3 "$1" ".backup '$2'"
	if [ "$(sqlite3 "$2" 'PRAGMA integrity_check')" != "ok" ]; then
		echo "backup: integrity_check FAILED for $2" >&2
		exit 1
	fi
	echo "backup: $1 -> $2 ($(du -h "$2" | cut -f1))"
}

sqlite_backup "$DATA_DIR/data.db" "$DEST/data.db"
sqlite_backup "$DATA_DIR/queue.db" "$DEST/queue.db"

if command -v nats >/dev/null && [ -d "$DATA_DIR/nats" ]; then
	if timeout 20 nats --server 127.0.0.1:4222 account backup "$DEST/jetstream" >/dev/null 2>&1; then
		echo "backup: jetstream -> $DEST/jetstream"
	else
		echo "backup: WARN JetStream backup skipped (server unreachable — stop the app or start it first)" >&2
	fi
else
	echo "backup: WARN JetStream backup skipped (no nats CLI or no $DATA_DIR/nats)" >&2
fi

echo "backup: done -> $DEST (data.db + queue.db integrity-checked; jetstream when reachable)"
