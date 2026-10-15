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
