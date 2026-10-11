-- Magic-link tokens: single-use passwordless logins.

-- name: CreateMagicToken :exec
INSERT INTO magic_tokens (token_hash, user_id, expires, created)
VALUES (?, ?, ?, ?);

-- name: GetMagicToken :one
SELECT * FROM magic_tokens WHERE token_hash = ?;

-- name: DeleteMagicToken :exec
DELETE FROM magic_tokens WHERE token_hash = ?;

-- name: DeleteMagicTokensForUser :exec
DELETE FROM magic_tokens WHERE user_id = ?;

-- name: DeleteExpiredMagicTokens :exec
DELETE FROM magic_tokens WHERE expires < ?;
