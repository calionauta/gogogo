-- 0001_init -- the todo unit's own schema, applied under the "todo" namespace.
--
-- The legacy bridge, documented because it is a deliberate choice and not a
-- missing piece: the core ledger (internal/dbschema/migrations/0001_init.sql)
-- still creates the `todos` table, because that history is append-only and
-- existing databases already hold rows in it. This migration is what makes the
-- table the todo unit's own: it creates the prefixed table in its final shape
-- (the post-0010 shape, with visibility) and carries every row across, so a
-- fresh database and an upgraded one end with the same schema and the same
-- data. The core "todos" table stays behind as the empty seed; all sqlc
-- queries read -- and only they may read -- ext_todo_todos.
--
-- The INSERT ... SELECT is data, not schema: sqlc ignores it when it reads this
-- directory as schema, and the recipe validator's verb table targets the
-- inserted-into table (ext_todo_todos), never the legacy source, so the copy
-- is legal. The statement is ordered after the CREATE and the indexes so a
-- missing legacy table fails loudly here rather than silently boot empty.

CREATE TABLE ext_todo_todos (
    id         TEXT PRIMARY KEY,
    owner      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title      TEXT NOT NULL,
    completed  INTEGER NOT NULL DEFAULT 0,
    idem_key   TEXT NOT NULL DEFAULT '',
    created    TEXT NOT NULL,
    updated    TEXT NOT NULL,
    visibility TEXT NOT NULL DEFAULT 'private'
        CHECK (visibility IN ('private', 'public'))
);

CREATE INDEX ext_todo_todos_owner_created ON ext_todo_todos (owner, created DESC);

-- Request-dedupe token (retry / double-click / offline replay). Partial so the
-- common empty value never collides.
CREATE UNIQUE INDEX ext_todo_todos_idem_key_unique ON ext_todo_todos (owner, idem_key) WHERE idem_key <> '';

CREATE INDEX ext_todo_todos_visibility ON ext_todo_todos (visibility);

INSERT INTO ext_todo_todos (id, owner, title, completed, idem_key, created, updated, visibility)
SELECT id, owner, title, completed, idem_key, created, updated, visibility
FROM todos;
