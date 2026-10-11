-- Boards and notes metadata: the list pages read these, never the docs.

-- name: ListBoardsByOwner :many
SELECT * FROM boards WHERE owner = ? ORDER BY updated DESC, doc_id DESC LIMIT 50;

-- name: UpsertBoard :exec
INSERT INTO boards (doc_id, owner, title, updated, created)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (doc_id) DO UPDATE SET updated = excluded.updated;

-- name: BumpBoardVersion :exec
UPDATE boards SET version = version + 1, updated = ? WHERE doc_id = ?;

-- name: ListNotesByOwner :many
SELECT * FROM notes WHERE owner = ? ORDER BY updated DESC, doc_id DESC LIMIT 50;

-- name: UpsertNote :exec
INSERT INTO notes (doc_id, owner, updated, created)
VALUES (?, ?, ?, ?)
ON CONFLICT (doc_id) DO NOTHING;


