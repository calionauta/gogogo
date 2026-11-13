# notes

**Owns.** Shared plain-text notes: the character ops, the server-merged text, the persisted snapshot, and the caret/typing presence around the editor (`/notes`, `/api/notes/*`).
**Does not own.** The CRDT transport itself (`collab`), the inbox row that announces a share (`notifications`), or drawings (`whiteboard`).

## Language

| term | means | not |
|---|---|---|
| note | one shared text document with an author | a chat message |
| content | the merged text the server holds | a client's unsaved buffer |
| caret | a peer's cursor position, reported by its owner | a selection made by the typist |

## Invariants

- Notes are shared: any signed-in user edits any note; `Delete` requires `canDeleteDoc` — author or admin.
- Text is merged server-side; a client never overwrites the document wholesale.
- A caret is written only by its owner's report and never rewritten through another client's ops.
- A standalone caret report is withheld while the reporter's local text differs from the server's.
