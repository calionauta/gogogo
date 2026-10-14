-- Casbin policy rows (RBAC_POLICY_SOURCE=db). See migration 0009 and
-- docs/rbac.md ("Dynamic permissions"). One row per rule.

-- name: ListCasbinRules :many
SELECT id, ptype, v0, v1, v2, v3, v4, v5 FROM casbin_rule ORDER BY id;

-- name: CountCasbinRules :one
SELECT COUNT(*) FROM casbin_rule;

-- name: InsertCasbinRule :exec
INSERT INTO casbin_rule (ptype, v0, v1, v2, v3, v4, v5)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: DeleteAllCasbinRules :exec
DELETE FROM casbin_rule;

-- name: DeleteCasbinRule :exec
DELETE FROM casbin_rule
WHERE ptype = ? AND v0 = ? AND v1 = ? AND v2 = ? AND v3 = ? AND v4 = ? AND v5 = ?;

-- name: DeleteCasbinRuleByID :exec
DELETE FROM casbin_rule WHERE id = ?;
