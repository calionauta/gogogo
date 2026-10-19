-- Boards and notes metadata: the list pages read these, never the docs.
--
-- Listing is SHARED: every signed-in user sees every doc, and the row
-- carries the author's email so the index can name who started it. Owner
-- is authorship, not a visibility filter - it is what the delete rule
-- reads (author or admin), never what the list scopes by.

-- name: ListBoards :many
SELECT boards.doc_id, boards.version, boards.updated, boards.created,
       boards.owner, users.email AS owner_email
FROM boards JOIN users ON users.id = boards.owner
ORDER BY boards.updated DESC, boards.doc_id DESC
LIMIT 50;

-- name: GetBoardMeta :one
SELECT boards.doc_id, boards.owner, users.email AS owner_email
FROM boards JOIN users ON users.id = boards.owner
WHERE boards.doc_id = ?;

-- name: UpsertBoard :exec
INSERT INTO boards (doc_id, owner, title, updated, created)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (doc_id) DO UPDATE SET updated = excluded.updated;

-- name: BumpBoardVersion :exec
UPDATE boards SET version = version + 1, updated = ? WHERE doc_id = ?;

-- name: DeleteBoard :exec
DELETE FROM boards WHERE doc_id = ?;

-- name: ListNotes :many
SELECT notes.doc_id, notes.updated, notes.created,
       notes.owner, users.email AS owner_email
FROM notes JOIN users ON users.id = notes.owner
ORDER BY notes.updated DESC, notes.doc_id DESC
LIMIT 50;

-- name: GetNoteMeta :one
SELECT notes.doc_id, notes.owner, users.email AS owner_email
FROM notes JOIN users ON users.id = notes.owner
WHERE notes.doc_id = ?;

-- name: UpsertNote :exec
INSERT INTO notes (doc_id, owner, updated, created)
VALUES (?, ?, ?, ?)
ON CONFLICT (doc_id) DO NOTHING;

-- name: DeleteNote :exec
DELETE FROM notes WHERE doc_id = ?;
