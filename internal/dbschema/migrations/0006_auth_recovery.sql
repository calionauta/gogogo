-- 0006_auth_recovery — single-use recovery codes for 2FA.
--
-- Ten codes per enrollment batch, shown once, consumed by DELETE. Only
-- hashes are stored: a leaked copy cannot be replayed, and used codes
-- vanish instead of flipping flags that could desync.

CREATE TABLE recovery_codes (
    code_hash TEXT PRIMARY KEY,
    user_id   TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created   TEXT NOT NULL
);

CREATE INDEX recovery_codes_user ON recovery_codes (user_id);
