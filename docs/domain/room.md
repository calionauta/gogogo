# room

**Owns.** Presence for one shared room: the roster of live connections, the exactly-one presenter lock, and the SSE pushes that keep every tab's view current (`/room`, `/room/acquire`).
**Does not own.** Identity and sessions (`auth`), the whiteboard document itself (`collab`, `whiteboard`), or the actor runtime — GoAkt is an implementation, with a `PeerSet` fallback.

## Language

| term | means | not |
|---|---|---|
| room | one presence scope with one roster | a chat channel with history |
| presenter | the single connection holding the lock | a role on the user row |
| connection | one SSE stream, refcounted per tab | a user |

## Invariants

- At most one presenter per room; acquiring again while holding succeeds and re-acquires.
- The roster is refcounted per connection and pushed on every change.
- A tab that hides keeps its stream; a closed tab leaves through its own announcement, not a timeout.
- A crash restart replays the live connections into the fresh grain.
