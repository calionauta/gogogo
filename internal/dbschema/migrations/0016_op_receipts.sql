-- 0016_op_receipts — exactly-once effect for a replayed browser mutation.
--
-- A write queued in the browser is delivered AT LEAST once: the network can
-- drop the response after the server committed, and the outbox sends the same
-- intent again. A read-then-flip mutation (toggle) is not idempotent, so the
-- second delivery used to revert the user's change minutes later.
--
-- The receipt says "(owner, key) was applied". It is claimed INSIDE the
-- transaction that applies the mutation, so "applied" and "recorded" cannot
-- disagree: a rollback leaves no receipt, and a commit leaves no window where
-- the work happened but the dedupe did not.
--
-- `created` backs the retention prune. A receipt only has to outlive the
-- outbox entry it protects (the client drops entries after a week), so the
-- daily housekeeping keeps a two-week window and the table stays tiny.
CREATE TABLE op_receipts (
    owner   TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    op_key  TEXT NOT NULL,
    created TEXT NOT NULL,
    PRIMARY KEY (owner, op_key)
);

CREATE INDEX op_receipts_created ON op_receipts (created);
