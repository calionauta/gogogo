-- 0014_session_user_agent — a human-readable device hint per session.
--
-- Raw user-agent, capped at write time. Empty for rows created before this
-- migration and for programmatic logins (CLI/tests) that carry no request.
-- It is the user's own data and expires with the session, never an auth claim.
ALTER TABLE sessions ADD COLUMN user_agent TEXT NOT NULL DEFAULT '';
