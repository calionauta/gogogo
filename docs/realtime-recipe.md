# Realtime UI recipe (for humans and coding agents)

How to add live UI to a feature without inventing a transport. Pick by
traffic type (matrix: `async-layers.md`), then copy the shape — the
reference implementations are the spec.

## Client patterns (all Datastar, no vanilla except bootstrap)

1. **Permanent stream** (ephemeral signals): hidden `@get(url, {permanent:
   true})` button clicked once on `datastar-ready` (+300ms fallback).
   Reference: `RealtimeStream` in `features/todo/components/realtime.templ`.
   Never `data-on:load` on div/body (v1 never fires it — silent no-op).
2. **List resync on record change**: shared `PbRealtimeResync` in
   `internal/components/realtime_resync.templ` (button id + fragment URL
   + topic params). Do not copy its script — call it.
3. **Fragment morph**: server returns HTML + `datastar-selector` +
   `datastar-mode: outer` headers (`handleListFragment` is the reference).
   Target element needs a top-level `id` + explicit selector or the patch
   throws `PatchElementsNoTargetsFound` (linted).
4. **Reactive trigger**: `data-effect="fn($signal)"` with a pure function
   in a *synchronous* script (module scripts load after Datastar init and
   throw — proven live by `scripts/todo-docwatch.test.mjs`). No polling
   loops for signal changes, ever.
5. **Failure release**: server-owned spinners need a client-side release
   for dead requests: `data-on:datastar-fetch` error stages on the form
   (reference: genui Ask form). Otherwise a network failure locks the UI
   until reload.

## Server patterns

- **Records** → PB realtime + fragment re-fetch (per-user scoped free).
- **Ephemeral** → own SSEHub + worker `hub.Send(clientID, …)`; stream
  loop with heartbeat (see `handleStream` in genui/whiteboard).
- **Worker return contract** → return `err` (pool retries) ONLY when the
  pool's hub is your feature's hub. Otherwise deliver the error result
  to your own hub and return nil: `llm.Chat` already retries transient
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
instead) · spinner has error release · empty + error states render.
