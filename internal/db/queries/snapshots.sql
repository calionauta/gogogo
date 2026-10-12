-- Doc snapshots: one resolved Loro snapshot per doc_id.

-- name: UpsertSnapshot :exec
INSERT INTO doc_snapshots (doc_id, snapshot, updated)
VALUES (?, ?, ?)
ON CONFLICT (doc_id) DO UPDATE SET snapshot = excluded.snapshot, updated = excluded.updated;

-- name: GetSnapshot :one
SELECT * FROM doc_snapshots WHERE doc_id = ?;
