-- name: ListTodos :many
SELECT id, title, completed, created_at, updated_at, owner_id
FROM todos
WHERE owner_id = ?1
ORDER BY created_at DESC;

-- name: ListTodosByFilter :many
SELECT id, title, completed, created_at, updated_at, owner_id
FROM todos
WHERE owner_id = ?1 AND completed = ?2
ORDER BY created_at DESC;

-- name: GetTodo :one
SELECT id, title, completed, created_at, updated_at, owner_id
FROM todos
WHERE id = ?1 AND owner_id = ?2;

-- name: CreateTodo :exec
INSERT INTO todos (id, title, completed, created_at, updated_at, owner_id)
VALUES (?1, ?2, ?3, ?4, ?5, ?6);

-- name: UpdateTodo :exec
UPDATE todos
SET title = ?3, completed = ?4, updated_at = ?5
WHERE id = ?1 AND owner_id = ?2;

-- name: DeleteTodo :exec
DELETE FROM todos WHERE id = ?1 AND owner_id = ?2;

-- name: ClearCompleted :execrows
DELETE FROM todos WHERE owner_id = ?1 AND completed = 1;

-- name: CountTodos :one
SELECT COUNT(*) FROM todos WHERE owner_id = ?1;
