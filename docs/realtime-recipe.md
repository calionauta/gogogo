# Realtime UI recipe (for humans and coding agents)

How to add live UI to a feature without inventing a transport. Pick by
traffic type (matrix: `async-layers.md`), then copy the shape — the
reference implementations are the spec.

## Client patterns (all Datastar, no vanilla except bootstrap)

**Signal ownership — read this before any merge.** A signal bound two-way to
an input (`data-bind="x"`) is **client-owned**: that element is the source of
truth while the user types. The server's copy is a mirror that always lags — a
value only reaches the server after a debounced round-trip — so **any server
merge that writes x overwrites live input**, and the element's next read (often
the debounced request itself) sees the clobbered value. The symptom is a
feature that silently does nothing.

The second half of the trap is serialization, and it is what makes this class
survive a first, wrong fix: a Go struct field with a JSON tag but **no
`omitempty`** still marshals (`"x":""`) for its zero value, so *removing the
field from the merge call does not stop the write*. `make datastar-lint` gates
both halves as errors — `CLIENT_BOUND_SIGNAL_NO_OMITEMPTY` and
`CLIENT_BOUND_SIGNAL_WRITTEN` (the latter a warning: an explicit write may be
intentional, e.g. clearing an input after a successful submit).

Live example: `newTitle` and `searchQuery` are both bound, both `omitempty`;
six background paths use to write `searchQuery` (stream handshake, record-event
re-list, workflow re-list, the mutation re-list, and `SetView` on every fetch)
and the search box lost its text mid-typing. Lived bug, fixed in the search
feature — the smoke caught it only intermittently, which is why the lint rule
exists.

A filter/scope tab click carries **no** `q` of its own, so its URL must append
`&q=$searchQuery`: the handler treats a missing `q` as "clear the stored
search", and omitting it drops the active search while the box still shows the
text.

1. **Permanent stream** (ephemeral signals): hidden `@get(url, {permanent:
   true})` button opened by `data-on:datastar-ready__document` (+300ms
   verified fallback for an already-fired ready). Reference: `RealtimeStream`
   in `features/todo/components/realtime.templ`.
   Never `data-on:load` on div/body (v1 never fires it — silent no-op).
   Never a blind click: every open resolves through `__gogogoOpenVerified`
   (see "Verified open" below).
2. **List resync on record change**: shared `ListResync` in
   `internal/components/realtime_resync.templ` (button id + fragment URL).
   Do not copy its script — call it.
3. **Fragment morph**: server returns HTML + `datastar-selector` +
   `datastar-mode: outer` headers (`handleListFragment` is the reference).
   Target element needs a top-level `id` + explicit selector or the patch
   throws `PatchElementsNoTargetsFound` (linted).
   **An APPENDED fragment must not carry the target's `id`.** A server-side
   patch resolves its selector to EVERY match, so appending the container
   component into its own container nests a second copy of the id and each
   later patch lands in both — one answer rendered twice from send #2 on
   (lived bug: chat appended `ChatList` into `#chat-messages`). Split the
   container from the body and append the body: `ChatList` owns the id and
   the guidance, `ChatThread` is the fragment (`features/chat/components`).
   State that leaves with the first message rides a signal the seed owns
   (`data-show="$chat_empty"`), never a second patch.
4. **Reactive trigger**: `data-effect="fn($signal)"` with a pure function
   in a *synchronous* script (module scripts load after Datastar init and
   throw — proven live by `scripts/todo-docwatch.test.mjs`). No polling
   loops for signal changes, ever.
5. **Failure release**: server-owned spinners need a client-side release
   for dead requests: `data-on:datastar-fetch` error stages on the form
   (reference: genui Ask form). Otherwise a network failure locks the UI
   until reload.

## Verified open (streams must prove they opened)

The opener clicks a Datastar `@get` it cannot observe — clicking before
`data-on:click` binds is a silent no-op that also poisoned `dataset.opened`,
leaving streams dead on slow clients (proven by aborting the Datastar bundle
and watching no stream ever start; the originating tab gets no live broadcast
by design, so a dead stream has no second chance). Every Datastar opener
(todo, basecoat, genui, chat) therefore resolves through
`window.__gogogoOpenVerified(buttonId)` (`web/resources/static/sse-open.js`):
re-click until `GET /api/stream/status?clientID=` reports the tab registered
in the shared hub, resolve on open, throw loudly past ~10s. A dead stream is
fixed or loud, never silent. The `data-on:datastar-ready__document` opener
itself was proven by probe to fire post-bind (a vanilla listener is not
needed for it — the allowlist shrank accordingly).
Raw-`EventSource` pages stay on native patterns instead: whiteboard replays
through `onopen` flush + browser auto-reconnect; notes relies on
auto-reconnect with only an error indicator (no flush hook — residual
candidate, not covered here).
Upstream watch: starfederation/datastar#1203 (silent SSE death, no native
restart primitive as of v1.2.2) — recheck periodically before building more
machinery here; a native open-ack or restart handle replaces the status poll.

## Server patterns

- **Records** → Hub record event + fragment re-fetch (visibility scoped free: private to owner tabs, shared to every login).
- **Ephemeral** → the shared queue hub (`chiQ.Hub()` — BootChi wires every
  stream and every worker to this ONE hub; a second hub object silently
  swallows worker feedback) + worker `hub.Send(clientID, …)`; stream
  loop with heartbeat (see `handleStream` in genui/whiteboard).
  Collaborative docs scope fan-out per doc (`BroadcastExceptToDoc` /
  `BroadcastToDoc` + a client-side `msg.doc` guard): a global broadcast
  let any board/note overwrite every other board/note's view.
- **Worker return contract** → return `err` (pool retries) ONLY when the
  failure is transient for the whole job. Otherwise deliver the error result
  to the originator and return nil: `llm.Chat` already retries transient
  faults natively, and a pool retry re-runs the whole job while
  broadcasting retry noise to a foreign tab (lived bug: Ask answers
  surfaced on Todo). One attempt, immediate user-visible result.
- **Mutations** → `fail()` on 500 (never `+err.Error()` — ruleguard
  blocks it); 400s carry the recovery reason for the same-trust client.
- **New `/api/*` URL in `.templ`/`.js`** → must match a registered Go
  route: `TestTemplAPIURLsHaveRoutes` + pre-commit hook enforce it.

## Checklist before review

`make templ && make datastar-lint` green · `go test -race <pkg>` green ·
no `setInterval` in new UI code · no `innerHTML` rendering (morph
instead) · appended fragments carry no target `id` · one
`#<container>` in the DOM after N patches · spinner has error release ·
empty + error states render · no server merge writes a `data-bind`
signal (and its struct field is `omitempty`) · every tab/scope URL that
can round-trip carries `q=$searchQuery`.
