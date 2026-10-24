# Recipe: API without auth, without Turso

This is a **fork recipe, not a trim preset** — and that is deliberate. Every
handler takes `dbgen.Queries`, every row is owner-scoped, every request
resolves an owner through `internal/auth`. Removing those is a second
product mode (unowned rows, memory stores, session-less middleware),
not a deletion the trim gate can prove. What follows is the exact map so
the fork is a day of work, not a week of archaeology. (Why no preset:
presets compose deletions the build gate proves; this changes contracts
the gate cannot see. A future `AUTH_MODE=single` fixed-owner mode would
collapse most of this recipe into a flag — proposed, not built.)

## A. No-auth (public API, keep the database)

Owner resolution has three choke points; everything else funnels through them:

1. `internal/auth/sessions.go` — `RequireSession` middleware (401 +
   cookie clear) and `Session` (resolve-or-redirect). Public endpoints
   simply do not mount it; nothing else changes.
2. Per-feature owner helpers — `ownerSession` in
   `features/todo/handlers/chi.go` (the reference shape), plus session reads in
   `features/notes/handlers/chi.go`, `features/whiteboard/handlers/chi.go`,
   `features/chat/handlers/chi.go`,
   `features/room/handlers/chi.go`, `features/landing/handlers/chi.go`,
   `features/config/handlers/chi.go`, `features/credits/handlers/chi.go`,
   `features/onboarding/onboarding.go`. Fourteen call sites total —
   replace each with a fixed owner constant (e.g. `"public"`) seeded once.
3. `internal/demo/` — the fresh-install demo account. Delete the call in
   `BootChi` (`internal/server/chi.go`) when there is no login form left
   to advertise.

What stays: password/magic/TOTP/OAuth routes keep working behind you
(delete `internal/auth/*` login handlers only if you want them gone —
`Session`/`RequireSession` are the load-bearing pair, the login UI is
not). What to update: `internal/server/chi_test.go` pins anon redirects
per route (303s become 200s on public endpoints); `docs/api-routes.md`
regenerates with `go test ./internal/capabilities/ -run
TestApiRoutesDocFresh -update`.

## B. No-Turso (keep auth, swap the driver)

`internal/db/` is the only data layer: `db.Open` + migrations +
`dbgen.*` callsites in six feature files (`features/todo/handlers/chi.go`,
`mutations.go`, `pages.go`, `bridge.go`, `features/notes/handlers/chi.go`,
`features/whiteboard/handlers/chi.go`). Replacing Turso means re-pointing
those callsites at your driver — there is no strategy switch by design
(handlers call `dbgen.*` directly; one data layer, no abstraction tax).
The local-file default (`TURSO_DB_PATH`, embedded, zero-ops) is already
the path of least resistance: most "no database" asks are really "no
database SERVER", which the default answers.

## C. Neither (memory-only single binary)

Combine A with in-memory stores per feature. Precedent: `chat/store`
(`MessageStore` interface + `MemStore`) — the seam already documents the
swap. Todo/notes/whiteboard have no memory stores; write one each
(roughly a day for the three, following the chat shape), point the
handlers at them, delete `internal/db/` + migrations. The queue (goqite)
is SQLite-backed too — either keep its file (simplest: it is an
implementation detail, not your schema) or replace background jobs with
inline calls.

## Proof after any variant

`go build ./...` then the affected suites (`internal/server`,
`internal/auth` if auth remains, `features/...`). The trim provenance
section in `AGENTS.md` does not apply (nothing was trimmed) — note the
fork in your own AGENTS.md instead so the next agent does not
"restore" what you removed on purpose.
