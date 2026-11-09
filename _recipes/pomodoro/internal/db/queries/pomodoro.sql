-- Queries over the recipe's own table. sqlc reads this file because the
-- installed copy lands in internal/db/queries/, which sqlc.yaml already scans;
-- the DDL it describes is the flat mirror `recipe install` writes to
-- internal/db/recipe_schema/pomodoro__0001_init.sql.
--
-- Every statement is owner-scoped: the caller's id is a parameter, never a
-- value the client could supply.

-- name: ListPomodoroSessions :many
SELECT id, owner, label, minutes, created_at
FROM ext_pomodoro_sessions
WHERE owner = ?
ORDER BY created_at DESC
LIMIT 50;

-- name: CreatePomodoroSession :exec
INSERT INTO ext_pomodoro_sessions (id, owner, label, minutes, created_at)
VALUES (?, ?, ?, ?, ?);
