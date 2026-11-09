-- The recipe's own table. `ext_<id>_` is the boundary the installer and the
-- runtime both enforce: a recipe never owns a core object, and uninstall has to
-- know exactly which rows came from where.
--
-- The shape follows the core schema: TEXT ids (UUIDv7, written by the caller),
-- TEXT timestamps in RFC3339 UTC, and an owner column so every read is scoped.

CREATE TABLE ext_pomodoro_sessions (
    id         TEXT    PRIMARY KEY,
    owner      TEXT    NOT NULL,
    label      TEXT    NOT NULL,
    minutes    INTEGER NOT NULL,
    created_at TEXT    NOT NULL
);

-- The page lists the caller's own sessions, newest first; this is the access
-- path that query uses.
CREATE INDEX ext_pomodoro_sessions_owner ON ext_pomodoro_sessions (owner, created_at);
