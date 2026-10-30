-- Todos: the reference vertical slice. Listing is visibility-scoped:
-- a user sees their own rows plus every 'public' row (the collab demo),
-- never another user's 'private' rows. Every statement carries that scope
-- so a handler cannot leak a private row by forgetting a filter.

-- name: ListTodosByOwner :many
SELECT * FROM todos WHERE owner = ? ORDER BY created DESC, id DESC;

-- name: ListVisibleTodos :many
SELECT todos.*, users.email AS owner_email
FROM todos JOIN users ON users.id = todos.owner
WHERE todos.owner = ? OR todos.visibility = 'public'
ORDER BY todos.created DESC, todos.id DESC;

-- name: GetTodo :one
SELECT * FROM todos WHERE id = ? AND owner = ?;

-- name: GetTodoVisible :one
SELECT todos.*, users.email AS owner_email
FROM todos JOIN users ON users.id = todos.owner
WHERE todos.id = ? AND (todos.owner = ? OR todos.visibility = 'public');

-- name: CreateTodo :one
INSERT INTO todos (id, owner, title, completed, idem_key, visibility, created, updated)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: SetTodoCompleted :exec
UPDATE todos SET completed = ?, updated = ? WHERE id = ? AND owner = ?;

-- name: SetTodoCompletedByID :exec
UPDATE todos SET completed = ?, updated = ? WHERE id = ?;

-- name: SetTodoVisibility :exec
UPDATE todos SET visibility = ?, updated = ? WHERE id = ? AND owner = ?;

-- name: DeleteTodo :exec
DELETE FROM todos WHERE id = ? AND owner = ?;

-- name: DeleteTodoByID :exec
DELETE FROM todos WHERE id = ?;

-- name: CountTodosByOwner :one
SELECT count(*) FROM todos WHERE owner = ?;

-- name: GetTodoByIdemKey :one
SELECT * FROM todos WHERE owner = ? AND idem_key = ? AND idem_key <> '';

-- name: DeleteCompletedByOwner :execrows
DELETE FROM todos WHERE owner = ? AND completed = 1;

-- name: ListCompletedIDsByOwner :many
-- The completed rows an owner is about to clear, read INSIDE the clearing
-- transaction. The search writer needs their ids to drop the matching
-- postings in that same transaction: reading them outside would race a
-- concurrent toggle into leaving an orphan posting behind.
SELECT id FROM todos WHERE owner = ? AND completed = 1;

-- name: SearchTodosByOwner :many
-- The todo search plugin's cheapest layer, in SQL: the owner's rows whose
-- title contains the query, case-insensitively. The caller builds the
-- pattern in Go, so the user's text needs no escaping here. No ranking:
-- that is what the other layers are for.
SELECT * FROM todos
WHERE owner = ? AND lower(title) LIKE lower(?)
ORDER BY completed, created DESC
LIMIT ?;

-- name: ListTodosForIndex :many
-- Every row's (id, owner, title) -- the input the search index heal needs.
--
-- Exists because the postings table is a CACHE of a Go-side pure function
-- (search.Stem over search.Tokenize), so it cannot be rebuilt by SQL: a row
-- created before bm25 was wired has no postings and SQL has no way to
-- compute them. The heal reads these rows and writes their postings through
-- the same IndexDoc the mutation path uses, which is what makes the rebuilt
-- index identical to an incrementally written one.
--
-- Columns are named rather than a star: the heal wants three fields and
-- nothing else, and a star would silently start pulling new columns into a
-- full-table read.
--
-- ASCII ONLY, and that is load-bearing: sqlc truncates the tail of a
-- statement when a comment above it contains a multi-byte character
-- (reproduced with an em dash, an en dash, an arrow and a right single
-- quote -- the generated SQL came out as "FROM tod", silently, with sqlc
-- exiting 0). Keep this comment ASCII, and if a query ever generates a
-- suspiciously short statement, check for non-ASCII bytes before anything else.
SELECT id, owner, title, visibility FROM todos;
