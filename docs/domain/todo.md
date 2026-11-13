# todo

**Owns.** The user's task list: create, toggle done, delete, clear completed, the AI completion suggestion, and the realtime stream that carries those changes to the owner's tabs.
**Does not own.** Inbox rows announcing a share (`notifications`), the chat panel that renders todo rows (`genui`), and presence/cursors (`collab`).

## Language

| term | means | not |
|---|---|---|
| todo | one task row, owned by one author | a project, a list |
| visibility | who may read the row: its owner, or every signed-in user | an ACL with per-user grants |
| owner | the user id stamped from the session | a client-supplied id |

## Invariants

- Every write goes through `features/todo/service`; a handler never issues SQL directly.
- `Create` stamps `owner` from the session; no request field can set it.
- `Delete` requires `canDeleteRow(role, requester, item)` — author or admin.
- `ClearCompleted` only removes the caller's own completed rows.
- AI suggest is optional: with no provider key the route answers without a model, never with a fake one.
