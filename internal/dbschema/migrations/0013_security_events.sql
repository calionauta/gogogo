-- 0013_security_events — durable security audit trail.
--
-- The same events already go to slog (Warn/Info); this table makes them
-- queryable from the admin page. No secrets or tokens: identifier/outcome
-- only, matching internal/auth/securitylog.go's contract.
CREATE TABLE security_events (
    id      TEXT PRIMARY KEY,
    event   TEXT NOT NULL,
    reason  TEXT NOT NULL DEFAULT '',
    email   TEXT NOT NULL DEFAULT '',
    user_id TEXT NOT NULL DEFAULT '',
    actor_id TEXT NOT NULL DEFAULT '',
    created TEXT NOT NULL
);
CREATE INDEX security_events_created ON security_events (created);
CREATE INDEX security_events_event_created ON security_events (event, created);
