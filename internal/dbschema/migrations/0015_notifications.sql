-- 0015_notifications — durable delivery records.
--
-- One row per delivery attempt per channel. The row keeps what happened AND
-- what did not: `status` plus `skip_reason` means "the user was not told" is a
-- record, not silence. That is the piece the queue and the SMTP mailer cannot
-- express today (they retry and forget).
--
-- (owner, transaction_id, channel) is unique. A retried workflow run cannot
-- deliver the same transaction twice on the same channel, which is what makes
-- at-least-once safe at this layer; the engine's idempotency key only dedups
-- run starts, not step retries.
--
-- `workflow` names the trigger (for example `todo-reminder`) so a reader can
-- tell a reminder from a digest without parsing the body. Ownership uses the
-- same TEXT uuid every owner column in this schema uses, and the channel is
-- kept as text because a provider may add one without a migration.
CREATE TABLE notifications (
    id             TEXT PRIMARY KEY,
    owner          TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    workflow       TEXT NOT NULL,
    transaction_id TEXT NOT NULL,
    channel        TEXT NOT NULL,
    status         TEXT NOT NULL,
    skip_reason    TEXT NOT NULL DEFAULT '',
    error_text     TEXT NOT NULL DEFAULT '',
    provider       TEXT NOT NULL DEFAULT '',
    subject        TEXT NOT NULL DEFAULT '',
    body           TEXT NOT NULL DEFAULT '',
    read_at        TEXT,
    created        TEXT NOT NULL
);

CREATE UNIQUE INDEX notifications_txn
    ON notifications (owner, transaction_id, channel);
CREATE INDEX notifications_owner_created
    ON notifications (owner, created DESC);
