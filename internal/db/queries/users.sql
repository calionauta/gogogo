-- Users: the identity table owned by internal/auth (argon2id hashes).

-- name: GetUser :one
SELECT * FROM users WHERE id = ?;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = ?;

-- name: CreateUser :one
INSERT INTO users (id, email, password_hash, name, created, updated)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = ?, updated = ? WHERE id = ?;

-- name: CountUsers :one
SELECT count(*) FROM users;
