-- name: ListRecipeStates :many
SELECT id, enabled FROM recipe_state;

-- name: SetRecipeState :exec
INSERT INTO recipe_state (id, enabled, updated_by, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    enabled = excluded.enabled,
    updated_by = excluded.updated_by,
    updated_at = excluded.updated_at;
