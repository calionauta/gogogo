#!/usr/bin/env bash
# restore.sh — restore a scripts/backup.sh directory into a data dir.
#
# Safety order (learned from Litestream restore guides): verify FIRST
# against a scratch copy, stop the app, move live files (INCLUDING
# -wal/-shm sidecars — a stale WAL next to a fresh DB confuses SQLite),
# then copy in. --dry-run verifies without touching anything and is the
# recommended monthly drill: an untested backup is a rumor.
#
# Usage: ./scripts/restore.sh [--dry-run] BACKUP_DIR [DATA_DIR]
#   DATA_DIR defaults to ./data. Refuses a live-looking DATA_DIR unless
#   --force is also given (a running app holds the WAL).
set -euo pipefail

DRY_RUN=0
FORCE=0
for a in "$@"; do
	case "$a" in
		--dry-run) DRY_RUN=1 ;;
		--force) FORCE=1 ;;
	esac
done
ARGS=()
for a in "$@"; do
	case "$a" in --dry-run|--force) ;; *) ARGS+=("$a") ;; esac
done
BACKUP="${ARGS[0]:-}"
DATA_DIR="${ARGS[1]:-./data}"
[ -n "$BACKUP" ] || { echo "restore: BACKUP_DIR required" >&2; exit 1; }
[ -d "$BACKUP" ] || { echo "restore: $BACKUP not a directory" >&2; exit 1; }
command -v sqlite3 >/dev/null || { echo "restore: sqlite3 CLI not found" >&2; exit 1; }

SCRATCH="$(mktemp -d)"
trap 'rm -rf "$SCRATCH"' EXIT
for db in data.db queue.db; do
	[ -f "$BACKUP/$db" ] || continue
	cp "$BACKUP/$db" "$SCRATCH/$db"
	if [ "$(sqlite3 "$SCRATCH/$db" 'PRAGMA integrity_check')" != "ok" ]; then
		echo "restore: $BACKUP/$db FAILED integrity_check — refusing" >&2
		exit 1
	fi
	echo "restore: verified $db (integrity ok)"
done
if [ "$DRY_RUN" = "1" ]; then
	echo "restore: dry-run ok — backup is intact, nothing touched"
	exit 0
fi
if [ -f "$DATA_DIR/data.db" ] && [ "$FORCE" != "1" ]; then
	echo "restore: $DATA_DIR looks live (data.db present)." >&2
	echo "restore: stop the app first, then re-run with --force." >&2
	exit 1
fi
mkdir -p "$DATA_DIR"
for db in data.db queue.db; do
	[ -f "$BACKUP/$db" ] || continue
	# Move sidecars aside with the live files: stale -wal/-shm next
	# to a restored DB is a corruption vector, not a speedup.
	for f in "$DATA_DIR/$db" "$DATA_DIR/$db-wal" "$DATA_DIR/$db-shm"; do
		[ -f "$f" ] && mv "$f" "$f.pre-restore-$(date +%s)"
	done
	cp "$BACKUP/$db" "$DATA_DIR/$db"
	echo "restore: installed $db"
done
echo "restore: done — start the app; each engine recovers on boot"
