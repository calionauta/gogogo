-- Sessions: cookie-backed logins. Only the hash of the cookie value is stored.

-- name: CreateSession :exec
INSERT INTO sessions (token_hash, user_id, expires, created)
VALUES (?, ?, ?, ?);

-- name: GetSession :one
SELECT * FROM sessions WHERE token_hash = ?;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = ?;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = ?;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires < ?;
