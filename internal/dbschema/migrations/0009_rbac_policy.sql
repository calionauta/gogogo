-- 0009_rbac_policy — Casbin rules in the database (optional policy source).
--
-- The embedded policy (internal/rbac/casbin/policy.csv) stays the default
-- and is compiled into the binary. With RBAC_POLICY_SOURCE=db the enforcer
-- loads its rules from this table instead, so an operator can edit the
-- policy at runtime (insert/delete rows, then reboot or reload) without
-- rebuilding the binary. The table is Casbin's standard `casbin_rule`
-- shape: one row per rule, a ptype ('p' policy / 'g' grouping) plus up to
-- six value columns.
--
-- The table is created on every install (migrations are driver-free and
-- always applied); it is only read when RBAC_POLICY_SOURCE=db. On first
-- use the embedded policy seeds it, so switching source does not change
-- the decisions until a row is edited.
CREATE TABLE IF NOT EXISTS casbin_rule (
    id    INTEGER PRIMARY KEY AUTOINCREMENT,
    ptype TEXT NOT NULL,
    v0    TEXT NOT NULL DEFAULT '',
    v1    TEXT NOT NULL DEFAULT '',
    v2    TEXT NOT NULL DEFAULT '',
    v3    TEXT NOT NULL DEFAULT '',
    v4    TEXT NOT NULL DEFAULT '',
    v5    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_casbin_rule_ptype ON casbin_rule(ptype);
