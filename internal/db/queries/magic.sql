-- Magic-link and email-action tokens: single-use, hashed, purpose-scoped.

-- name: CreateMagicToken :exec
INSERT INTO magic_tokens (token_hash, user_id, purpose, expires, created)
VALUES (?, ?, ?, ?, ?);

-- name: GetMagicToken :one
SELECT * FROM magic_tokens WHERE token_hash = ?;

-- name: DeleteMagicToken :exec
DELETE FROM magic_tokens WHERE token_hash = ?;

-- name: DeleteMagicTokensForUser :exec
DELETE FROM magic_tokens WHERE user_id = ?;

-- name: DeleteMagicTokensForUserPurpose :exec
DELETE FROM magic_tokens WHERE user_id = ? AND purpose = ?;

-- name: DeleteExpiredMagicTokens :exec
DELETE FROM magic_tokens WHERE expires < ?;

-- name: LatestMagicTokenForUser :one
SELECT * FROM magic_tokens WHERE user_id = ? ORDER BY created DESC LIMIT 1;

-- name: LatestMagicTokenForUserPurpose :one
SELECT * FROM magic_tokens WHERE user_id = ? AND purpose = ? ORDER BY created DESC LIMIT 1;
