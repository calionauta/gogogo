-- 0004_flat_identity — identity owned by the app, not the library.
--
-- 0002 created limen_* tables for the limen auth library; step 6 replaces
-- limen with the flat auth in internal/auth (argon2id + sessions table),
-- so those five tables never served a request either and they go. The app
-- users/sessions tables return in their 0001 shape (TEXT UUID identity,
-- the same shape every owner column in the schema already uses), and
-- todos.owner moves back to TEXT REFERENCES users(id).
--
-- SQLite cannot change a column type in place, so todos is rebuilt. Only
-- rows resolving to the restored users table are carried over; the INTEGER
-- owners of the removed limen model never equal a TEXT uuid, so in practice
-- the copy moves zero rows and fails safe instead of corrupting FKs.

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

-- Sessions replace framework auth tokens. Only the hash of the cookie value
-- is stored: a leaked database copy cannot be replayed as a login.
CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires    TEXT NOT NULL,
    created    TEXT NOT NULL
);

CREATE INDEX sessions_user ON sessions (user_id);

CREATE TABLE todos_new (
    id        TEXT    PRIMARY KEY,
    owner     TEXT    NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title     TEXT    NOT NULL,
    completed INTEGER NOT NULL DEFAULT 0,
    idem_key  TEXT    NOT NULL DEFAULT '',
    created   TEXT    NOT NULL,
    updated   TEXT    NOT NULL
);

INSERT INTO todos_new (id, owner, title, completed, idem_key, created, updated)
SELECT id, owner, title, completed, idem_key, created, updated
FROM todos
WHERE EXISTS (
    SELECT 1 FROM users WHERE users.id = todos.owner
);

DROP TABLE todos;

ALTER TABLE todos_new RENAME TO todos;

CREATE INDEX todos_owner_created ON todos (owner, created DESC);

-- Request-dedupe token (retry / double-click / offline replay). Partial so the
-- common empty value never collides.
CREATE UNIQUE INDEX todos_idem_key_unique ON todos (owner, idem_key) WHERE idem_key <> '';

DROP TABLE limen_sessions;

DROP TABLE limen_users;

DROP TABLE limen_accounts;

DROP TABLE limen_verifications;

DROP TABLE limen_rate_limits;
