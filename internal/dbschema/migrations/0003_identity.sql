-- 0003_identity — unify identity on limen.
--
-- 0001 created app-owned users/sessions tables for the direct-auth path
-- (argon2id + sessions table). Step 3 chose limen instead, so those two
-- tables never served a request: nothing outside internal/db tests reads
-- them, and no shipped database holds rows (the server boot has never
-- opened the Turso file). Keeping them would leave two identity stores and
-- two session stores for every future reader to trip over, so they go, and
-- todos.owner moves to the one identity that remains: limen_users
-- (INTEGER ids, the type limen's sql adapter reads back).
--
-- SQLite cannot change a column type in place, so todos is rebuilt. Rows
-- whose TEXT owner cannot resolve to a limen_users id belong to the deleted
-- identity model and are not carried over: the guarded copy below moves
-- only resolvable rows, so the migration fails safe instead of writing
-- FK-violating or zero-id rows.
--
-- (No PRAGMA foreign_keys toggling here: applyMigration runs inside a
-- transaction, where that pragma is a documented no-op. FKs stay enforced
-- the whole way; DROP TABLE on a referenced parent performs SQLite's
-- implicit delete, which is why the copy runs first.)

CREATE TABLE todos_new (
    id        TEXT    PRIMARY KEY,
    owner     INTEGER NOT NULL REFERENCES limen_users (id) ON DELETE CASCADE,
    title     TEXT    NOT NULL,
    completed INTEGER NOT NULL DEFAULT 0,
    idem_key  TEXT    NOT NULL DEFAULT '',
    created   TEXT    NOT NULL,
    updated   TEXT    NOT NULL
);

INSERT INTO todos_new (id, owner, title, completed, idem_key, created, updated)
SELECT id, CAST(owner AS INTEGER), title, completed, idem_key, created, updated
FROM todos
WHERE EXISTS (
    SELECT 1 FROM limen_users WHERE limen_users.id = CAST(todos.owner AS INTEGER)
);

DROP TABLE todos;

ALTER TABLE todos_new RENAME TO todos;

CREATE INDEX todos_owner_created ON todos (owner, created DESC);

-- Request-dedupe token (retry / double-click / offline replay). Partial so the
-- common empty value never collides.
CREATE UNIQUE INDEX todos_idem_key_unique ON todos (owner, idem_key) WHERE idem_key <> '';

DROP TABLE sessions;

DROP TABLE users;
