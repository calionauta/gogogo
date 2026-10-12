-- TOTP enrollment state on users; OAuth links in their own table.

-- name: SetTOTPSecret :exec
UPDATE users SET totp_secret = ?, totp_verified = 0, updated = ? WHERE id = ?;

-- name: UpdateTOTPSecret :exec
-- Seal upgrades only: rewrites the secret (plaintext -> sealed) without
-- touching the verified flag. SetTOTPSecret above is enrollment (resets).
UPDATE users SET totp_secret = ?, updated = ? WHERE id = ?;

-- name: VerifyTOTPFlag :exec
UPDATE users SET totp_verified = 1, updated = ? WHERE id = ?;

-- name: ClearTOTP :exec
UPDATE users SET totp_secret = '', totp_verified = 0, updated = ? WHERE id = ?;

-- name: GetOAuthAccount :one
SELECT * FROM oauth_accounts WHERE provider = ? AND subject = ?;

-- name: LinkOAuthAccount :exec
INSERT INTO oauth_accounts (provider, subject, user_id, created)
VALUES (?, ?, ?, ?)
ON CONFLICT (provider, subject) DO NOTHING;
