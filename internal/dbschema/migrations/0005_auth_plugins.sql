-- 0005_auth_plugins — second-factor and federated identity tables.
--
-- Magic-link tokens are single-use by construction (consumed by DELETE, so
-- no used flag exists to drift). TOTP state lives on users: an empty
-- totp_secret means 2FA off, and totp_verified guards the window between
-- enrollment and confirmation so an abandoned setup never locks the user
-- out. OAuth links live in their own table keyed by (provider, subject).

ALTER TABLE users ADD COLUMN totp_secret TEXT NOT NULL DEFAULT '';

ALTER TABLE users ADD COLUMN totp_verified INTEGER NOT NULL DEFAULT 0;

CREATE TABLE magic_tokens (
    token_hash TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires    TEXT NOT NULL,
    created    TEXT NOT NULL
);

CREATE INDEX magic_tokens_user ON magic_tokens (user_id);

CREATE TABLE oauth_accounts (
    provider   TEXT NOT NULL,
    subject    TEXT NOT NULL,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created    TEXT NOT NULL,
    PRIMARY KEY (provider, subject)
);

CREATE INDEX oauth_accounts_user ON oauth_accounts (user_id);
