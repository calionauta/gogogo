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
both halves: `CLIENT_BOUND_SIGNAL_NO_OMITEMPTY` is an **error** (the tag
survives a reader fixing the obvious merge line), `CLIENT_BOUND_SIGNAL_WRITTEN`
an **error** too (a write is the bug, whichever door it came through).

Live example: `newTitle` and `searchQuery` are both bound. The search box lost
its text mid-typing because six call sites wrote `searchQuery` (the stream
handshake, both list endpoints, the mutation re-list, and the `/search`
response echoing its own `?q=`). The same class hit the composers: the create
response cleared `newTitle`, and both ask acks cleared `chatPrompt`/
`genuiPrompt` — a late answer erased whatever the user had started typing, which
is exactly when an ask ack lands. In every case the fix is the same shape: the
response raises only the in-flight guard, and the **client** clears the box
after its own request succeeds.

**How the initial seed works, and why the server writes nothing.** The page
still has to put `?q=` into the box once, and the clean way is *not* to write
the signal: keep the field off the wire (`json:"-"` on `Signals.SearchQuery`),
render it into the input's `value=`, and let the element seed the signal.
`data-bind` calls `mergePaths([[path, get(el, type)]], { ifMissing: true })`
upstream (`bind.ts`), so the input creates `$searchQuery` from its own value on
first render and owns it from then on. The server never touches a client-owned
signal, so there is no suppression to write and no write to get wrong:

```go
// models.go — off the wire entirely
SearchQuery string `json:"-"`
```
```
<!-- todo_list.templ — the element seeds the signal -->
<input name="q" value={ signals.SearchQuery } data-bind="searchQuery"
       data-on:input__debounce.300ms="... @get('/api/todos/search?q=' + ...)">
```

`make datastar-lint` reports **zero** `CLIENT_BOUND_SIGNAL_WRITTEN` findings on
this tree, so the count is re-derivable rather than trusted. (Before v0.16.0 the
lint would have flagged the `data-signals` carry — and once it learned to read
`json:"-"`, the old suppression went stale and `SUPPRESS_UNUSED` failed the
gate. That is the design working: the escape hatch cannot outlive its subject.)

The same rule covers the mark-up door: a server-rendered `data-signals:key` or
`data-signals={obj}` re-applies on every morph and clobbers live input exactly
like a Go merge. If you must ship a signals attribute for a bound signal, add
`ifmissing` (`data-signals:key__ifmissing`, `data-signals__ifmissing`).

The echo is worth stating separately, because it is the form a reader does not
suspect: deriving a display signal from the request's own parameter feels like
echoing state, not writing it. `/search` therefore seeds nothing — the box is
seeded once by the input itself (`displaySignalsWithQuery` renders the value
into `value=`), and later responses patch rows only (`displaySignals` never
writes the query). A late response to `?q=kes` then cannot re-impose "kes" over
the "kestrel" the user has typed since.

A filter/scope tab click carries **no** `q` of its own, so its URL must append
`&q=$searchQuery`: the handler treats a missing `q` as "clear the stored
search", and omitting it drops the active search while the box still shows the
text.

1. **Stream open** (ephemeral signals): the ONE shared opener,
   `components.StreamOpener(buttonID, streamURL, openWhenHidden)` in
   `internal/components/stream_opener.templ` — hidden `@get(url, {retry:
   'always'})` button opened by `data-on:datastar-ready__document` (+300ms
   verified fallback for an already-fired ready), plus the per-tab clientID
   bootstrap. Every feature and skin calls it; do not copy it (copies drifted
   into a removed helper and a nonexistent `{permanent}` option).
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
5. **Append-only feed**: end the fragment with `ic.AutoScroll()`. The
   anchor travels inside the patch that carries the new item, and
   Datastar re-runs `data-init` on every node an element morph inserts,
   so the newest item scrolls itself into view with no listener and no
   vanilla JS. Reference: `ChatThread` (append) and `GenuiResult`
   (outer replace). Put it inside the branch that actually renders the
   item — an anchor in the empty state scrolls the page on load.
6. **Failure release**: server-owned spinners need a client-side release
   for dead requests: `data-on:datastar-fetch` error stages on the form
   (reference: genui Ask form). Otherwise a network failure locks the UI
   until reload.

## Verified open (streams must prove they opened)

The opener clicks a Datastar `@get` it cannot observe — clicking before
`data-on:click` binds is a silent no-op, leaving streams dead on slow
clients (proven by aborting the Datastar bundle and watching no stream ever
start; the originating tab gets no live broadcast by design, so a dead
stream has no second chance). Every Datastar page renders the ONE shared opener
(`internal/components/stream_opener.templ`: hidden button + clientID
bootstrap) and resolves through `window.__gogogoOpenVerified(buttonId)`
(`web/resources/static/sse-open.js`): click, then wait for the stream's
OWN first frame — the `streamOpen` ack signal every stream sends
(`internal/datastar.AckStreamOpened`) — and re-click while it is missing,
throwing loudly past ~10s. The ack proves the click bound, the handler ran,
and the browser APPLIED the response; an HTTP status probe proved only that
the hub held a registration, cost a second request per open, and said
nothing about delivery.

Datastar owns the rest of the lifecycle (v1.0.4): a `@get` stream closes
when the tab is hidden and reopens when it is visible (`openWhenHidden`
defaults to false for GET), requests cancel by method+URL, and network
errors retry with backoff. What it does NOT retry is a graceful server close,
so every opener passes `retry: 'always'` (#1073, shipped in v1.0.0-RC.7):
Datastar itself retries a clean close with backoff, resets the count on each
200, and sends `Last-Event-ID`. The room opts into
`openWhenHidden: true` because presence is the CONNECTION — a hidden tab
that drops its stream would look like a departure.
`sse-open.js` keeps two jobs Datastar cannot do: the ack loop for the initial
bind race, and `__gogogoSSERefresh` on `online` / mobile `resume` / bfcache
`pageshow`, where a deep-slept socket can hang without an error (#1203).

Raw-`EventSource` pages stay on native patterns instead: whiteboard replays
through `onopen` flush + browser auto-reconnect; notes relies on
auto-reconnect with only an error indicator (no flush hook — residual
candidate, not covered here).
Upstream watch: #1073 shipped (`retry: auto|error|always`; `permanent` never
existed); #1203 is OPEN with no milestone (restart handle for silent socket
death — maintainer's advice is `resume`/`pageshow`, which we implement). No
issue tracks a native open-ack; the full re-check list on a bundle bump lives
in `skills/gogogo-coding-standards/references/datastar.md`.

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
signal (and its struct field is off the wire — `json:"-"`, or `omitempty`
where the server may set it later) · every tab/scope URL that
can round-trip carries `q=$searchQuery` · append-only feeds end in
`ic.AutoScroll()` (and their empty state does not).
