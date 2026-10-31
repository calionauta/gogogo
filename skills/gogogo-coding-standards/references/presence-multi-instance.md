# Collaborative presence (multi-instance)

Presence answers "who is on this document?". It is the one piece of realtime
state that is BOTH per-connection and shared across instances, which is why it
has two layers and why each layer alone produces a plausible-looking bug.

## The invariant

**One seam, two wires, and the count must be the union.**

| Layer | API | Subject | Carries |
|---|---|---|---|
| Frames | `hub.BroadcastToDoc` / `BroadcastExceptToDoc` | `app.docbridge.<doc>` | join, leave, cursor, caret, draft, count |
| Roster | `collab.FederatedPresence` | `app.roster.<doc>` | each instance's full local roster |

Whiteboard and notes share ONE `collab.PresenceSeam`
(`internal/collab/presence_seam.go`): the GoAkt grain per document when the
engine runs, the in-process `PeerSet` otherwise. Two features, one owner per
document.

## Why both wires exist (each fails alone)

- **Frames only** → a peer's join/leave/cursor crosses instances, but every
  `count` frame is computed from the local roster. Two tabs on different pods
  both read "1 online" while sharing the board. The frame arrived; the number
  lied.
- **Roster only** → the count is the union, but leaves and cursors never
  arrive (they are frames, not roster snapshots). A closed tab keeps a presence
  dot until the lease expires.

Both were observed. `TestNotesPresenceCrossesInstances` fails with either
layer removed, and so does `TestWhiteboard_PresenceCrossesInstances` /
`TestWhiteboard_CrossInstanceCursorStaysOnItsDoc` — one per layer, each proven
by mutation.

## Rules

1. **Doc scope on both wires.** Subjects are per document, ids are single NATS
   tokens (reject a dotted id rather than truncate it). A doc-blind merge is
   invisible in a single-doc test: to catch it you need a doc-1 tab on EACH
   instance, so the remote entries a blind merge would fold in actually exist.
   That is why the negative-control tests look over-connected.
2. **Snapshots, never deltas.** A roster message is the sender's FULL local
   set. A dropped publish is healed by the next one; there is no sequencing,
   no ack, no replay.
3. **Lease for liveness, not timers for membership.** Republish every 10s,
   expire a remote entry after 3 missed leases (30s). A SIGKILLed pod cannot
   announce its own death. On orderly shutdown publish an EMPTY roster so peers
   drop those connections immediately.
4. **Ignore your own echo by instance id**, not by subject: the publisher
   receives its own roster back and must not re-merge it.
5. **The roster is connection-scoped, the label is identity.** `Peers` holds
   clientIDs (two tabs of one user are two peers); `User` is the display name
   from `collab.DisplayName` (server-derived email, never a client field).
6. **The local seam is swappable, never nil.** The GoAkt grain is an upgrade
   (`upgradePresenceToActor` / `UseLocal`), not a requirement: trimming the
   engine or `GOAKT_ENABLED=false` must leave a working `PeerSet` roster.
7. **Doc deletion forgets the roster.** `ForgetDoc` clears the local set, the
   published set and the merged remote view; a reconnect to a recycled id must
   not resurrect departed connections.
8. **Never compute presence from a timer or a poll.** Presence IS the
   connection. A rejoin is a new connection; a departure is a closed one (plus
   the browser's `pagehide` beacon, because an EventSource reconnect races the
   teardown — see `handleLeavePresence`).

## Attachment order (composition root)

`BootChi` builds `bootPresence(hub)` before NATS exists (the wrapper is a plain
passthrough to the local seam until attached). Then, in `cmd/web/main.go`, after
`startNATS`:

```go
stack.AttachJetStream(...)
stack.AttachEphemeral(nc)          // Send/Broadcast — a DIFFERENT traffic class
stack.AttachDocBridge(nc)          // doc-scoped frames
stack.AttachPresenceFederation(ctx, nc) // roster union + lease goroutine
```

`AttachEphemeral` and `AttachDocBridge` must stay on separate subject families:
one shared subject delivers every doc event twice.

## Red-proof checklist for a new presence path

1. Two instances, one NATS, one board each side.
2. Join on A → B's pill lists BOTH ids (fails without federation).
3. Leave on A → B's pill drops it (fails without the doc bridge).
4. Cursor on A for doc-1 → reaches B's doc-1 tab, never B's doc-2 tab (fails
   with a doc-blind merge or a global subject).
5. Delete the doc → `List` is empty and a reconnect starts from zero.
6. Mutate each layer independently and confirm the matching test goes red.
   A test that survives both mutations is testing a helper, not the contract.
