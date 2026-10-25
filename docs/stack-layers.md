# Stack in layers, not silos

Everything you need to build a modern web app, in a single binary. This page is
the **benefits side**: what is in the box and what each piece buys you. The
honest other half — what was rejected, what each choice costs, and when to
revisit — lives in [Costs, tradeoffs & comparisons](stack-tradeoffs.md). For the
layered async topology see [Seven async layers](async-layers.md).

| Layer | Choice | What it buys |
|-------|--------|--------------|
| **Language** | Go 1.27 | Fast compilation, easy deploy, lean runtime |
| **Database** | Turso Database (SQLite-compatible, sqlc) | Zero-config Turso, owned queries, file-local storage — no separate service. See [Turso vs SQLite vs the server databases](stack-tradeoffs.md#database-turso-and-the-server-databases-it-replaces). |
| **Auth** | owned argon2id sessions (`gogogo_auth`) | Cookie sessions in your database; no auth service, no framework surface. |
| **Authz / RBAC** | [Casbin](https://casbin.org) | Roles and policy in one model, enforced on routes and UI (`admin` / `user`), embedded policy by default and DB-backed when evolving — see [RBAC](rbac.md). |
| **Routing** | [chi](https://go-chi.io) v5 | Zero deps, stdlib-shaped middleware chains, URL params and router-level 405s — chosen for ergonomics, not speed ([why](stack-tradeoffs.md#routers-chi-for-ergonomics-not-speed)). |
| **Templating** | [Templ](https://templ.guide) | Type-safe Go components, generated at build time |
| **Reactive UI** | [Datastar](https://data-star.dev) (SSE) | Server-rendered over SSE, single ~12 KiB client. CSS built once via the Tailwind v4 CLI; no JS framework build step. |
| **CSS / themes** | [DaisyUI v5](https://daisyui.com) + TailwindCSS, four palettes | One bundle; a palette switch is a theme, not a second bundle. See [UI themes](ui-themes.md). |
| **Diagrams** | [Pele](https://pele.run) (vendored, Mermaid syntax) | Mermaid fences in a Chat answer render to themeable SVG in the browser: synchronous, no runtime dependencies, coloured by the app's CSS variables. Progressive enhancement — without the script the source stays readable as code — and only the chat page loads it (692 KB raw / 258 KB gzip). The vendored copy is pinned by `web/resources/pele_version_test.go` |
| **Task queue** | [goqite](https://github.com/maragudk/goqite) + SSE Hub | goqite executes with backoff; SSE Hub streams progress to the browser, no Redis |
| **Retries** | [avast/retry-go v4](https://github.com/avast/retry-go) | Exponential backoff with jitter, no boilerplate |
| **Durable workflows** | [DagNats](https://github.com/danmestas/dagnats) | Multi-step durable workflows as declarative JSON over NATS JetStream |
| **LLM SDK** | [GoAI](https://github.com/zendev-sh/goai) | Any provider: OpenAI, Anthropic, Groq, Ollama…; provider-aware retry and streaming |
| **Typed AI flows** | [Genkit](https://genkit.dev) (Go, zero provider plugins) | Flows wrap model calls with validated schemas, streaming, OTel traces and a Dev UI — generation reaches providers only through the injectable LLM client, so the billing choke point holds |
| **Classifier judge** | [go-jev](https://github.com/mattn/go-jev) + any Jev-compatible API | Decisions as typed choices with confidence instead of prose verdicts; orders of magnitude cheaper than a generative judge, most accurate on prose |
| **AI credits + BYOK** | [ai-credits](https://github.com/calionauta/ai-credits) | Optional SQLite ledger: managed LLM reserve/settle billing, monthly entitlements, Stripe top-ups, encrypted per-user BYOK relay metering |
| **Real-time** | [NATS JetStream](https://nats.io) | Multi-user realtime, cross-instance broadcast |
| **Offline sync** | Service Worker + NATS Leaf Node + idempotency key | The page keeps working offline: mutations queue in the browser and replay on reconnect, deduped by an idempotency key the database backs with a unique index (`OFFLINE_SYNC_ENABLED=false` turns it off) |
| **Collaboration (CRDT)** | [loro-go](https://github.com/aholstenson/loro-go) | Conflict-free merging of whiteboard/notes state; converges offline edits with no last-write-wins data loss |
| **Room ownership** | [GoAkt](https://github.com/Tochemey/goakt) | One addressable owner per room: connection-scoped roster pushed over SSE plus exactly-one presenter lock, supervised |
| **Hand-drawn canvas** | [Rough.js](https://roughjs.com) (embedded) | Minimalist sketchy whiteboard rendering, embedded in the binary so it is removed with the whiteboard feature |
| **Date picker** | [Cally](https://wicky.nillia.ms/cally/) + DaisyUI's `calendar` styles | A web component that defines `<calendar-date>`: keyboard- and screen-reader-accessible, localized through `Intl`, themed through CSS parts (no React — DaisyUI's other calendar targets are React Day Picker and Vanilla Calendar Pro). Loaded as a module by the one page with the filter, so its 38 KB never reach the landing or the shell; the presets keep the filter usable if the script fails. Pinned by `web/resources/cally_version_test.go` |
| **Secrets** | [age](https://age-encryption.org) + `~/.secrets/` | Local encryption, no vault, no cloud |
| **IDs** | [google/uuid](https://github.com/google/uuid) | Stable request/job IDs |
| **Observability** | `log/slog` + [OpenTelemetry](https://opentelemetry.io) | Structured logs and traces from one wiring point (`internal/observability`) — no vendor SDK in the build |
| **Live reload** | [Air](https://github.com/air-verse/air) | `make dev` regenerates templ and restarts the binary |
| **Native window (PoC)** | [gogpu/ui](https://github.com/gogpu/ui) (pure Go, zero CGO) | `cmd/gui`: a second frontend over the same Turso/sqlc store — no HTTP, no webview. See [Desktop & Mobile](desktop-mobile.md). |
| **Linting** | [golangci-lint](https://golangci-lint.run) + [datastar-lint](https://github.com/calionauta/datastar-lint) | The linters pinned in `.golangci.yml`, across correctness, errors, resources, cancellation, tests, complexity and style — categories and what each catches in [Code quality](code-quality.md). Datastar attribute/signal mistakes are caught by `datastar-lint` (`make datastar-lint`); project-specific footguns are `ruleguard` rules in [`rules/rules.go`](https://github.com/calionauta/gogogo/blob/master/rules/rules.go), which CI fails on. |
| **CI/CD** | GitHub Actions | `ci.yml` (lint + test + build) + `deploy.yml` (cross-compiles the prod binary and ships it to the server on `master`; `make docker-image` pushes a multi-arch image to ghcr.io, locally, when you want one). Three off-path maintenance workflows: `stelow-drift.yml` (weekly, fails when the vendored stelow standards pin is stale) and `stelow-auto-bump.yml` (weekly, opens the bump PR) and `desktop.yml` |

## How the pieces combine

The layers are not independent choices — they are designed to work together:

- A handler returns a **Templ** component; **Datastar** decides whether the
  response is a full page or an SSE patch.
- Background work goes to **goqite**; the worker streams a **toast** back to
  the originating browser through the **SSE Hub**, addressed by `clientID`.
- Multi-step processes go to **DagNats**, whose state is durable on **JetStream**.
- Collaborative state goes through **Loro CRDT**, so offline edits merge
  without last-write-wins data loss.
- Record changes fan out through the SSE Hub record event, scoped by row
  visibility (private rows to the owner's tabs, shared rows to every login).
- Single-owner decisions go to **GoAkt grains**: one addressable owner per room
  (roster, locks, supervision).
- Access decisions go to **Casbin**: one model, one policy, enforced before the
  handler runs ([RBAC](rbac.md)).
- Multi-role AI runs go to a **Genkit flow**: writer and critic generate
  through `llm.Client` (metered once per call), a Jev-compatible judge
  classifies terminally, and the verdict streams back over ephemeral SSE,
  discarded — per-role cost visible in the trace.

## Why no frontend framework?

Datastar gives you reactivity with no client-side framework and no build step.
The client is a single ~12 KiB script; state lives in signals that the server
reads and writes over SSE. You get the ergonomics of a reactive UI without a
`node_modules` tree, a bundler, or hydration — and the HTML that arrives over
the wire is the same HTML the server rendered, so there is no second source of
truth to drift.

CSS is the one build step, and it is a build step you run explicitly
(`make css`), not one your users pay for at runtime: the compiled
`app.min.css` is embedded into the binary with `//go:embed`.

The rule behind the lightness: **the backend is the source of truth and
the browser holds ephemeral intent plus cache, never authority.** Records
live in Turso, documents in Loro snapshots, jobs in goqite — the
client renders what the server ships and reports what the user did. That
is what keeps every feature between 0 and ~48 KB of JS (measured per
feature: [Client weight](features.md#client-weight)). The honest boundary:
offline outboxes and textarea drafts do live client-side, but as cache to
be reconciled, never as truth to be defended.

The alternative, and what it would cost, is in
[Datastar vs htmx vs a JS framework](stack-tradeoffs.md#datastar-and-not-htmx-and-not-a-js-framework).

## Native dependencies: pure Go first

A native dependency is anything that is not pure Go: a C library via
CGO, a shared object loaded through `purego`, a WASM engine like the
one inside ncruces. The ladder, in order — stop at the first rung that
works:

1. **Pure Go.** No toolchain beyond `go`, no link-time surprises.
2. **Toolchain-free FFI.** `purego` (dlopen of a C ABI, like tursogo
   uses) or a WASM runtime (like ncruces). `go build` stays
   compiler-free and `CGO_ENABLED=0` keeps working.
3. **CGO, with a written justification.** `loro-go` lives here today —
   there is no pure-Go CRDT of its quality, so the repo pays for gcc in
   CI (`ci.yml`) and documents it. A new CGO dependency must say who
   installs the compiler (CI, every dev machine, cross-compile), whether
   the link is static, and what the alpine runtime has to provide.

This is a ladder, not a ban: rung 3 exists because rung 2 cannot cover
everything (inline C, unstable ABIs, libraries with no dlopen story).
What the ladder forbids is reaching rung 3 *first* out of familiarity.

Two hard rules once native code is in:

- **One FFI runtime per binary.** `purego`'s `fakecgo` collides with
  goffi's at CGO-less link time (measured: `cmd/gui` + tursogo, fixed by
  opening through ncruces instead — see `internal/dbschema`). A binary
  that links goffi (gui, desktop) cannot also link purego without CGO.
- **New native surface updates the build matrix.** CGO additions touch
  `desktop.yml` (gcc), the cross story (`GOOS=linux` from mac must still
  work), and the package-audit scan — never just `go.mod`.

### Why `ncruces/go-sqlite3`?

It is the pure-Go (no cgo) SQLite engine for the paths that open plain
SQLite files directly (the native window registration, the credits test
databases) — registered as the `sqlite3` `database/sql` driver. The app
database itself (tables + queue) runs on `tursogo` (driver name `turso`).

Being cgo-free means clean cross-compilation of the production binary (the
deploy workflow builds `GOOS=linux` from any runner).

#### Extensions: available, none loaded

The ncruces tree ships extensions (`fts5`, `rtree`, `vec1` — SQLite's own
vector extension — `bloom`, `unicode`, …), but nothing in this repo
registers or loads any of them (verified: no `LoadExtension`, no
`USING fts5/vec1` anywhere). The door is open — semantic search over
todos via `vec1` would be one `Register` call away — but until a feature
needs it, no extension loads. If you add one, document it here and pin
a test that queries through it: an unloaded extension directory is not
a capability.

The native-dependency cost — Turso's ~124 MB platform-lib module, the
dlopen/`scratch` trap, the musl build tag — is documented in
[Turso Database: the cost, stated plainly](stack-tradeoffs.md#turso-database-the-cost-stated-plainly).

## Why the flat stack?

Every data path is owned code with no framework between the handler and
Turso: sqlc-generated queries called directly (a wrong query is a
compile error, not a runtime mismatch), an explicit schema in
`internal/dbschema/migrations`, owned argon2id sessions behind a single
`gogogo_auth` cookie, and visibility scoping in the queries themselves.
Nothing is configured through a dashboard, a rule language, or a hook
chain — there is less to learn because there is less there. The
decision record and the removal narrative live where history lives
([architecture-decision.md](architecture-decision.md), CHANGELOG
version sections); this page describes only what runs today.

The cost side of that decision — what we own, what Turso is pre-1.0, and
which framework surface was rejected — is in
[The flat stack — the cost](stack-tradeoffs.md#the-flat-stack-the-cost).

## Related

- [Costs, tradeoffs & comparisons](stack-tradeoffs.md) — what was rejected and
  what each choice costs.
- [Features](features.md) — every capability and its runtime opt-out.
- [Async layers](async-layers.md) — the seven layers and why each exists.
- [Code quality for LLM agents](code-quality.md) — the lint set behind these choices.
