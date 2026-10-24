-- 0011_auth_email_lifecycle — email verification/reset tokens and idle sessions.
--
-- magic_tokens gains a purpose: login links, email verification links and
-- password-reset links share one single-use, hashed token table. Existing
-- rows default to 'login', so the upgrade is behavior-preserving.
ALTER TABLE magic_tokens ADD COLUMN purpose TEXT NOT NULL DEFAULT 'login';
CREATE INDEX magic_tokens_purpose ON magic_tokens (purpose, user_id);

-- users.email_verified_at: empty means unverified. Password signup leaves it
-- empty; OAuth provider-verified email and the explicit verification link set it.
ALTER TABLE users ADD COLUMN email_verified_at TEXT NOT NULL DEFAULT '';

-- sessions.last_seen: idle-timeout input. Existing rows fall back to created.
ALTER TABLE sessions ADD COLUMN last_seen TEXT NOT NULL DEFAULT '';
