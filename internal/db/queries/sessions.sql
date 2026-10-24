-- Sessions: cookie-backed logins. Only the hash of the cookie value is stored.

-- name: CreateSession :exec
INSERT INTO sessions (token_hash, user_id, session_id, last_seen, expires, created, user_agent)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetSession :one
SELECT * FROM sessions WHERE token_hash = ?;

-- name: UpdateSessionLastSeen :exec
UPDATE sessions SET last_seen = ? WHERE token_hash = ?;

-- name: ListSessionsByUser :many
SELECT session_id, created, expires, last_seen, user_agent FROM sessions
WHERE user_id = ? AND session_id <> '' ORDER BY created DESC;

-- name: DeleteSessionByID :exec
DELETE FROM sessions WHERE user_id = ? AND session_id = ?;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = ?;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = ?;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires < ?;
