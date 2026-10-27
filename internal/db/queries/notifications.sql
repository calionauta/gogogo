-- Notifications: durable delivery records for the notifications feature.
-- Every statement is scoped by owner, and none takes an owner from a caller
-- that could have obtained it from a request: handlers pass the session owner.
--
-- The inbox LIST is in_app only. An email row is the same table, one channel
-- per row, so the audit trail is complete without the inbox showing a second
-- copy of a message the user already saw.

-- name: CreateNotification :one
-- Returns sql.ErrNoRows on a duplicate (owner, transaction_id, channel).
-- That is the dedupe contract, not a failure: a retried step is a no-op.
INSERT INTO notifications (
    id, owner, workflow, transaction_id, channel, status, skip_reason,
    error_text, provider, subject, body, created
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (owner, transaction_id, channel) DO NOTHING
RETURNING *;

-- name: ListNotificationsForOwner :many
SELECT * FROM notifications
WHERE owner = ? AND channel = 'in_app'
ORDER BY created DESC, id DESC
LIMIT ?;

-- name: CountUnreadInApp :one
SELECT count(*) FROM notifications
WHERE owner = ? AND channel = 'in_app' AND read_at IS NULL;

-- name: MarkNotificationRead :execrows
UPDATE notifications
SET read_at = ?
WHERE id = ? AND owner = ? AND read_at IS NULL;

-- name: MarkAllNotificationsRead :execrows
UPDATE notifications
SET read_at = ?
WHERE owner = ? AND channel = 'in_app' AND read_at IS NULL;

-- External channels go from pending to sent/error/skipped. The provider's own
-- ack (send-time) is the only evidence a synchronous transport can give, so the
-- row records the attempt; docs/notifications.md says so.
-- name: SetNotificationStatus :execrows
UPDATE notifications
SET status = ?, skip_reason = ?, error_text = ?
WHERE id = ? AND owner = ?
