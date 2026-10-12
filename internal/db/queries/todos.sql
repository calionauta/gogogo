-- Todos: the reference vertical slice. Every statement is owner-scoped, so a
-- handler cannot leak another tenant's row by forgetting a filter.

-- name: ListTodosByOwner :many
SELECT * FROM todos WHERE owner = ? ORDER BY created DESC, id DESC;

-- name: GetTodo :one
SELECT * FROM todos WHERE id = ? AND owner = ?;

-- name: CreateTodo :one
INSERT INTO todos (id, owner, title, completed, idem_key, created, updated)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: SetTodoCompleted :exec
UPDATE todos SET completed = ?, updated = ? WHERE id = ? AND owner = ?;

-- name: DeleteTodo :exec
DELETE FROM todos WHERE id = ? AND owner = ?;

-- name: CountTodosByOwner :one
SELECT count(*) FROM todos WHERE owner = ?;

-- name: GetTodoByIdemKey :one
SELECT * FROM todos WHERE owner = ? AND idem_key = ? AND idem_key <> '';

-- name: DeleteCompletedByOwner :execrows
DELETE FROM todos WHERE owner = ? AND completed = 1;
