-- 0007_collab — board/note metadata plus Loro snapshot storage.
--
-- The CRDT docs themselves live in memory (Loro) and converge over the
-- SSE hub; the database holds only what must survive a restart: the
-- metadata lists (boards, notes) and the resolved snapshots. Snapshots
-- are content-addressed by doc_id (one row per doc, upserted), so a
-- rewrite can never fork a doc into two states.

CREATE TABLE boards (
    doc_id  TEXT PRIMARY KEY,
    owner   TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title   TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 0,
    updated TEXT NOT NULL,
    created TEXT NOT NULL
);

CREATE INDEX boards_owner_updated ON boards (owner, updated DESC);

CREATE TABLE notes (
    doc_id  TEXT PRIMARY KEY,
    owner   TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    updated TEXT NOT NULL,
    created TEXT NOT NULL
);

CREATE INDEX notes_owner_updated ON notes (owner, updated DESC);

CREATE TABLE doc_snapshots (
    doc_id   TEXT PRIMARY KEY,
    snapshot BLOB NOT NULL,
    updated  TEXT NOT NULL
);
