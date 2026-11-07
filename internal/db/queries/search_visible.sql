-- name: SearchTodosVisible :many
-- The shared-search half of layer 1: own rows plus public rows, same
-- case-insensitive substring and order as SearchTodosByOwner. The caller
-- builds the pattern in Go, so the user's text needs no escaping here.
SELECT * FROM ext_todo_todos
WHERE (owner = ? OR visibility = 'public') AND lower(title) LIKE lower(?)
ORDER BY completed, created DESC
LIMIT ?;

