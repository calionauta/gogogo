-- Recovery codes: single-use 2FA fallbacks, hashes only.

-- name: CreateRecoveryCode :exec
INSERT INTO recovery_codes (code_hash, user_id, created)
VALUES (?, ?, ?);

-- name: ConsumeRecoveryCode :execrows
DELETE FROM recovery_codes WHERE code_hash = ? AND user_id = ?;

-- name: DeleteRecoveryCodesForUser :exec
DELETE FROM recovery_codes WHERE user_id = ?;

-- name: CountRecoveryCodes :one
SELECT count(*) FROM recovery_codes WHERE user_id = ?;
