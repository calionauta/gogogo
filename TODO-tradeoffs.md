# TODO — trade-offs left open (this is a temporary plan file)

Written at the end of the "unify metrics dashboard" session so the items below
are decisions with a trigger, not a vague "later". Delete this file when every
row is either done or explicitly dropped.

Mark `[x]` when done, and record the measurement that justified it.

## 1. `register.Deps` is wide

`Deps` now carries ~20 fields (`Cfg`, `Auth`, `Queries`, `Hub`, `Queue`, both
LLM clients, `OnShutdown`, `SQLDB`, slots, collab stores, four handle holders,
`Panels`, `Enabled`, `Installed`, `Events`). Every recipe sees the whole thing.

- **Why it stays:** splitting it into sub-structs (`Core`, `Collab`, `LLM`,
  `Refs`) is more SOLID and self-documenting, but it touches every recipe's
  `Register`, the generated registration and every test fixture at once — a
  mechanical refactor with no behaviour change, and the wrong thing to bundle
  into a feature change.
- **Trigger:** a third consumer needs a *new* dependency family (e.g. a second
  storage engine), or `Deps` passes 25 fields.
- **Shape when done:** one embedded struct per family, `Deps.Core`,
  `Deps.Collab`, `Deps.LLM`, so a recipe names only what it reads. Keep
  `Allowed`/`Installed` on the top level (they are build facts, not services).
- **Blocked (attempted 2026-11-14):** embedded families make the boot literal
  mix positional (embedded) and keyed values, which Go forbids. A fully
  positional literal compiles but fails `govet composites` (unkeyed fields of
  another package) at every call site, and the keyed form fails
  `modernize embedlit` — so `make lint-safe` cannot go green with embedded
  families. The honest version is named families (`d.Core.Cfg`) at every call
  site, which is exactly the mechanical refactor already priced above; the
  trigger stands.

## 2. `cmd/gogogo-mcp` pulls `tursogo` (CGO) transitively

The MCP reads `recipe_state` through the same query layer as the server, so the
stdio binary now links the Turso driver.

- **Why it stays:** one source of truth for the state table beats a second
  implementation; the MCP is a developer tool (never shipped to users), and it
  already needs the checkout it operates on.
- **Trigger:** someone wants the MCP distributed as a static single binary, or
  a CI job needs it without CGO.
- **Shape when done:** MCP talks to a running server over the product API
  (`/api/ext/invoke`) or reads the receipt files only. That is a real surface
  decision (auth, threat model), not a refactor — see `docs/mcp.md`.

## 3. `api` shape: mount-time pruning is not compile-time removal

`APP_SHAPE=api` now skips page routes in `MountFeature` and `mountHome`, so a
page path 404s from the tree; the page handlers, their components and their
RBAC rows still compile into the binary.

- **Why it stays:** true compile-time pruning means build tags or a generated
  route manifest per shape, which forks the composition root the template
  exists to keep single. The product contract is already real; the remaining
  gain is binary size and a smaller dead surface.
- **Measure before doing it:** `go build ./cmd/web` size delta and
  `go tool nm` count of page handlers, with and without an `api` build tag.
  If the delta is small (likely: handlers are thin, templates dominate), the
  answer is "no".
- **Trigger:** a deployed API binary where the page surface is a security
  review finding, or a measured size win worth the maintenance.
- **Shape when done:** a generated `routes_api.gen.go` per shape, checked by
  `check-shape`; never per-feature `if` statements.

## 4. `recipe_state` is read in two layers — done 2026-11-14

`internal/server/runtimeRecipeStates` (boot switch) and
`features/config/handlers.applyRecipeStates` (admin table) both call
`ListRecipeStates` and both default "missing row = enabled".

- **Why it stays:** two callers, one query, and the defaults are three lines —
  a package for that would be an abstraction with no second implementation.
- **Trigger:** a third caller (a CLI admin command, an MCP mutation) or a
  change to the default semantics.
- **Shape when done:** `internal/recipestate` with `Enabled(ctx, id)`,
  `All(ctx)` and one place for the missing-row rule.
- **Done:** `internal/recipestate` (`States`, `Load`, `Enabled`); the boot
  switch and the admin Units table both call it, and the env override stays in
  the switch. Proof: `TestMissingRowIsEnabled` / `TestLoadReadsTheSwitch`, and
  the first goes red when the default is flipped to "missing = disabled".

## 5. Event delivery is best-effort, not a durable outbox

The bus delivers on its goroutine and drops on a full buffer; the webhook
retries twice and then gives up. A crash between the write and the delivery
loses the event.

- **Why it stays:** an outbox row + worker is a migration, generated queries,
  a drainer and a DLQ — the right shape, but not for a seam with no production
  consumer yet.
- **Trigger:** the first webhook consumer that needs at-least-once (a billing
  or CRM integration), or any report of a missing delivery that matters.
- **Shape when done:** an `event_outbox` row inserted in the same transaction
  as the domain write, drained by the existing `queue` with exponential backoff
  + jitter, idempotency key unchanged. See `docs/hooks-analytics.md`.

## 6. Receipts do not declare `events:`

A recipe can emit (`Deps.Events`) but the receipt's `Declaration` has no
`events` list, so no gate can compare claimed events with emitted ones.

- **Why it stays:** a declaration without a consumer is a field that rots. The
  gate needs a taxonomy to check against, and that taxonomy is the thing this
  session deliberately did not invent.
- **Trigger:** two recipes emitting events, or an operator asking "which
  recipes notify us?".
- **Shape when done:** `Declaration.Events []string`, validated by
  `recipe check` against the event names in the recipe's code (the same textual
  check routes use today), plus a row in the authoring skill.

## 7. CI scope: the shared-file fallback and the lint cache

`scripts/changed-packages.sh` falls back to **all** packages for `config/`,
`internal/capabilities/`, `internal/db/`, `go.mod`, `go.sum`, `Makefile`. The
recommendation was to let `config/` and `internal/capabilities/` seed a package
and expand through the dependents closure instead. Separately, the checks job
runs `golangci-lint-action` with `skip-cache: true`.

- **Why it stays:** both changes make the gate narrower or faster, and both
  can silently miss a failure. Neither can be validated on a co-tenanted
  8 GB host — they need a measured PR on CI (green + wall-clock delta), not a
  guess. The comments in `docs/local-ci.md` say why the fallback exists.
- **Trigger:** the next session that is allowed to run `ci-local` and compare
  two full CI runs.
- **Done when:** PR A (closure) shows the same failures as the all-packages
  run on a deliberately broken dependent; PR B (cache) shows green across 10
  consecutive runs. Revert either if flakiness returns.

## 8. Tenant-scoped hooks

Recommendation #3 said no "tenant hook" until a `tenant_id` model exists. The
app is single-tenant per instance (`users.role`, one database).

- **Why it stays:** the name would lie. Nothing in the schema can enumerate
  tenants, so a per-tenant webhook target has no key to hang on.
- **Trigger:** a real multi-tenant deployment (shared database, scoped rows).
- **Shape when done:** outbox row carries `tenant_id`; the webhook sink
  resolves the target from config per tenant; RBAC gains the tenant axis.

## 9. Deliberately not built

- **Client-side pageviews.** A page render is already a server request and
  already logged; a beacon pageview would double-count and add a request per
  navigation. The beacon carries affordance clicks only.
- **Analytics on GET interactions.** Reads are the access log's job; a
  firehose to an analytics provider is a bill, not a signal.
- **A generic trigger engine** over arbitrary functions/affordances. It grows
  the security surface, makes replay hard and becomes flag debt. Five event
  names and two sinks are the contract.
- **Auth lifecycle events on the bus** (`auth.login`, `auth.logout`, role
  change). They already exist in `security_events` + the access log; publishing
  them needs `auth.Auth` to hold the bus. Trigger: an external consumer (a
  customer-success webhook on first login) — then it is a setter and two emit
  sites, not a redesign.
