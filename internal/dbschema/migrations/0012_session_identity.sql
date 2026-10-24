-- 0012_session_identity — stable session ids for device management.
--
-- Old rows keep an empty id (not listed); new sessions get a UUIDv7 from
-- dbschema.NewID. Revocation is scoped by user_id AND session_id.
ALTER TABLE sessions ADD COLUMN session_id TEXT NOT NULL DEFAULT '';
CREATE INDEX sessions_user_session ON sessions (user_id, session_id);
