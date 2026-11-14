# notifications

**Owns.** The inbox: delivery rows addressed to one owner, the unread badge, read/unread transitions, scheduled reminders, and the live stream that keeps the rail and the page in step (`/notifications`, `/api/notifications/*`).
**Does not own.** The transport that actually sends (`delivery`), the workflow that decides *when* (`dagnats`), or the surface being announced (`todo`, `notes`, …).

## Language

| term | means | not |
|---|---|---|
| delivery | one row per (owner, transaction, channel) | a sent message |
| unread | a delivery the owner has not marked read | an undelivered row |
| skip reason | why a delivery did not happen | an error string for a failed send |

## Invariants

- Reads are owner-scoped by SQL, never filtered in Go after a global query.
- A delivery is deduped on `(owner, transaction_id, channel)`; a repeat is the same row.
- Status and `skip_reason` are persisted, so a delivery that did not happen is still a record.
- The reminder delay lives in the durable workflow, so a restart does not drop it.
