# chat

**Owns.** The conversation: the user's prompt, the persisted history, the answer as text plus components, and the SSE stream that renders it live (`/chat`, `/api/chat/ask|clear|stream`).
**Does not own.** The component catalog or the answer envelope (`genui`), the model client (`llm`), or the rows a panel hydrates (each feature's own store).

## Language

| term | means | not |
|---|---|---|
| conversation | the prompt/answer rows persisted per owner | a session, a thread id supplied by the client |
| answer | prose plus validated components patched into the live column | raw model HTML |
| panel | a component whose rows come from a slice's own store | a copy of another page |

## Invariants

- History is owner-scoped: a read never returns another caller's rows.
- An answer is validated before it renders; a payload that fails degrades to its prose, never to raw JSON.
- A panel's rows are hydrated by the slice that owns them; the model picks the slice and the filter, never the content.
- The stream is one per page load and carries no periodic fetch.
