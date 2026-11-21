# whiteboard

**Owns.** Shared boards: the drawing ops, the in-progress shape relayed over the presence channel, the persisted snapshot, and the live index (`/whiteboard`, `/whiteboard/new`, `/api/whiteboards`, `/api/whiteboard/*`).
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
- A board is created by one helper with two faces: `POST /api/whiteboards` answers the new id as JSON for a headless caller, `GET /whiteboard/new` redirects the browser to the page. A failed insert returns an error instead of an id whose row does not exist, because the update path requires the row.
- The one GET that writes is claim-on-open: opening an id with no row registers it to the first signed-in visitor (a shared link, two tabs). It is insert-only — `ON CONFLICT DO NOTHING` — so it never rewrites an existing author; `TestWhiteboardClaimNeverRewritesAnExistingAuthor` pins that bound, which is what keeps the write acceptable behind a page load.
- Cursor identity is stamped by the server; a client cannot claim another's cursor.
- Idle cursors expire on a TTL and a closed tab leaves immediately.
