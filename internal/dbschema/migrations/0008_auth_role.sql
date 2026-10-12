-- 0008_auth_role — coarse role on users for the RBAC middleware.

-- Two values matter: 'admin' and everything else ('user'). Anonymous is
-- not a row (no session). Default 'user' so existing rows keep working;
-- promotion happens at boot from ADMIN_EMAILS, never at signup (a fresh
-- password proves nothing about email ownership, so self-promotion must
-- be impossible). Unknown values are treated as 'user' at enforcement
-- time — least surprise, documented in internal/rbac.
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user';
