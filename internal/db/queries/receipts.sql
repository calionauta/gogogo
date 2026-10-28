-- Operation receipts: the dedupe token for a mutation that may be delivered
-- more than once.
--
-- A queued browser write is at-least-once: the response can be lost after the
-- commit, and the outbox replays the SAME intent with the SAME key. Claiming
-- the key inside the mutation's transaction makes the second delivery a no-op.
-- A keyless request skips the receipt entirely (opt-in contract).

-- name: ClaimOpReceipt :execrows
INSERT INTO op_receipts (owner, op_key, created)
VALUES (?, ?, ?)
ON CONFLICT (owner, op_key) DO NOTHING;

-- name: DeleteOpReceiptsBatchBefore :execrows
DELETE FROM op_receipts WHERE rowid IN (
    SELECT rowid FROM op_receipts AS old
    WHERE old.created < ? ORDER BY old.created LIMIT ?
);
