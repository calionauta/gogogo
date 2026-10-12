-- 0001_init — core schema: identity, sessions, todos.
--
-- Column names are the wire names (owner, completed, idem_key), and every
-- table is owner-scoped so a client only ever reads its own rows.
--
-- Timestamps are TEXT in RFC3339 UTC (see db.TimeFormat), always written by
-- the caller so no engine function has to exist for the insert to succeed.

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL,
    password_hash TEXT NOT NULL DEFAULT '',
    name          TEXT NOT NULL DEFAULT '',
    created       TEXT NOT NULL,
    updated       TEXT NOT NULL
);

-- Email is stored lowercased by the caller, so a plain unique index is enough
-- and no index-time function is needed.
CREATE UNIQUE INDEX users_email_unique ON users (email);

-- Sessions hold only the hash of the cookie value: a leaked database copy
-- cannot be replayed as a login.
CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires    TEXT NOT NULL,
    created    TEXT NOT NULL
);

CREATE INDEX sessions_user ON sessions (user_id);

CREATE TABLE todos (
    id        TEXT    PRIMARY KEY,
    owner     TEXT    NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title     TEXT    NOT NULL,
    completed INTEGER NOT NULL DEFAULT 0,
    idem_key  TEXT    NOT NULL DEFAULT '',
    created   TEXT    NOT NULL,
    updated   TEXT    NOT NULL
);

CREATE INDEX todos_owner_created ON todos (owner, created DESC);

-- Request-dedupe token (retry / double-click / offline replay). Partial so the
-- common empty value never collides.
CREATE UNIQUE INDEX todos_idem_key_unique ON todos (owner, idem_key) WHERE idem_key <> '';
