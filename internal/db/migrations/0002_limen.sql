-- 0002_limen — tables owned by the limen auth library.
--
-- Column-for-column with limen v0.2.3's default schema introspection
-- (users/session/account/verification/rate-limit), with every table renamed
-- to the `limen_` prefix via Schema options in internal/auth. The prefix
-- keeps limen's identity tables apart from the app tables in 0001 (`users`,
-- `sessions`), which carry different columns under the same historic names.
--
-- Type mapping: limen String/Text/Map -> TEXT (maps marshal to JSON),
-- Int/Int64 -> INTEGER, Time -> TEXT (limen binds time.Time; Turso/SQLite
-- stores it in RFC3339 form). Default integer IDs, so INTEGER PRIMARY KEY
-- AUTOINCREMENT. No soft-delete column: it only exists when customized, and
-- it is not. No public_id column: public IDs are disabled by default.
--
-- Sessions reference limen_users (RESTRICT on delete: wiping a user must be
-- an explicit operation, not a cascade side effect).

CREATE TABLE limen_users (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    email             TEXT NOT NULL,
    password          TEXT,
    email_verified_at TEXT,
    first_name        TEXT,
    last_name         TEXT,
    created_at        TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX idx_limen_users_email ON limen_users (email);

CREATE TABLE limen_sessions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    token       TEXT NOT NULL,
    user_id     INTEGER NOT NULL REFERENCES limen_users (id) ON DELETE RESTRICT ON UPDATE CASCADE,
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at  TEXT NOT NULL,
    last_access TEXT NOT NULL,
    metadata    TEXT
);

CREATE UNIQUE INDEX idx_limen_sessions_token ON limen_sessions (token);
CREATE INDEX idx_limen_sessions_user_id ON limen_sessions (user_id);

CREATE TABLE limen_accounts (
    id                       INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id                  INTEGER NOT NULL REFERENCES limen_users (id) ON DELETE RESTRICT ON UPDATE CASCADE,
    provider                 TEXT NOT NULL,
    provider_account_id      TEXT,
    access_token             TEXT NOT NULL,
    refresh_token            TEXT,
    access_token_expires_at  TEXT,
    scope                    TEXT NOT NULL,
    id_token                 TEXT,
    created_at               TEXT NOT NULL,
    updated_at               TEXT NOT NULL
);

CREATE INDEX idx_limen_accounts_user_id_provider ON limen_accounts (user_id);
CREATE UNIQUE INDEX idx_limen_accounts_provider_account ON limen_accounts (provider, provider_account_id);

CREATE TABLE limen_verifications (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    subject    TEXT NOT NULL,
    value      TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX idx_limen_verifications_value ON limen_verifications (value);
CREATE INDEX idx_limen_verifications_subject ON limen_verifications (subject);

CREATE TABLE limen_rate_limits (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    key             TEXT NOT NULL,
    count           INTEGER NOT NULL,
    last_request_at INTEGER NOT NULL
);

CREATE UNIQUE INDEX idx_limen_rate_limits_key ON limen_rate_limits (key);
