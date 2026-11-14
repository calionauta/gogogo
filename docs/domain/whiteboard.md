# whiteboard

**Owns.** Shared boards: the drawing ops, the in-progress shape relayed over the presence channel, the persisted snapshot, and the live index (`/whiteboard`, `/api/whiteboard/*`).
**Does not own.** The CRDT transport and cursor presence (`collab`), text documents (`notes`), or the room's presenter lock (`room`).

## Language

| term | means | not |
|---|---|---|
| board | one shared canvas with an author | a private workspace |
| op | one committed drawing mutation | a whole re-render |
| live shape | the shape under the pointer, relayed before release | a saved element |

## Invariants

- Boards are shared: every signed-in user opens any board; the index lists all of them with their author.
- `Delete` requires `canDeleteDoc` — author or admin — which also drops the snapshot.
- Cursor identity is stamped by the server; a client cannot claim another's cursor.
- Idle cursors expire on a TTL and a closed tab leaves immediately.
