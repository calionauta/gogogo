-- 0010_todo_visibility — shared vs private todos for the collab demo.
--
-- Every todo gains a visibility flag: 'private' (default, only the owner
-- lists it — the pre-0010 behaviour) or 'public' (every authenticated
-- user lists it, which is what lets demo1 and demo2 prove live collab
-- from two browsers). Existing rows default to 'private', so the upgrade
-- changes nothing visible until a user shares something.
--
-- Visibility is a LISTING scope, not a permission: what a role may DO
-- with a visible row stays in the Casbin policy (viewer still cannot
-- delete, even a public row; only the owner or an admin deletes).
-- CHECK constraint keeps the column to the two known values so a typo
-- can never silently hide a row from every list.

ALTER TABLE todos ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private'
    CHECK (visibility IN ('private', 'public'));

CREATE INDEX todos_visibility ON todos (visibility);
