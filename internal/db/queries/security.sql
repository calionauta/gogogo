-- Security audit events: durable twin of the slog vocabulary.

-- name: InsertSecurityEvent :exec
INSERT INTO security_events (id, event, reason, email, user_id, actor_id, created)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ListSecurityEvents :many
SELECT * FROM security_events ORDER BY created DESC, id DESC LIMIT ?;

-- name: ListSecurityEventsPage :many
SELECT * FROM security_events
ORDER BY created DESC, id DESC
LIMIT ? OFFSET ?;

-- name: ListSecurityEventsByEvent :many
SELECT * FROM security_events
WHERE event = ?
ORDER BY created DESC, id DESC
LIMIT ? OFFSET ?;

-- name: ListSecurityEventNames :many
SELECT DISTINCT event FROM security_events ORDER BY event;

-- name: SecurityEventCountsSince :many
SELECT event, reason, count(*) AS count
FROM security_events
WHERE created >= ?
GROUP BY event, reason
ORDER BY count DESC, event, reason;

-- name: DeleteSecurityEventsBefore :exec
DELETE FROM security_events WHERE created < ?;
