-- 0012_collab_shared - collaborative docs are SHARED, not per-owner.
--
-- Boards and notes were listed with `WHERE owner = ?`, so a second login
-- saw an empty index and could only reach a doc through a copied URL.
-- The demo needs the opposite: every signed-in user opens every doc, the
-- list names the author, and only the author (or an admin) deletes it.
--
-- Sharing is a LISTING change, not a permission change: the owner column
-- stays exactly as it is (it is the author, and the row a delete rule
-- reads), and no data moves. These indexes serve the new global ordering
-- (`ORDER BY updated DESC`, no owner prefix).
--
-- Delete is a hard delete of the metadata row plus the snapshot; both
-- live in this schema, so there is no orphan state to reconcile.

CREATE INDEX boards_updated ON boards (updated DESC, doc_id DESC);

CREATE INDEX notes_updated ON notes (updated DESC, doc_id DESC);
