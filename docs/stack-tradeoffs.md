# Costs, tradeoffs & comparisons

[Stack in layers](stack-layers.md) states what is in the box and what each
piece buys you. This page is the honest other half: for each choice, what was
rejected, what it costs to keep, and the trigger that would make it lose.
Nothing here is a claim that the alternative is bad — it is why it did not
win *for a single-binary, owned-code template*.

> Rule of the template: a choice earns its place by what it buys, not by what
> it beats. Read the benefits first, then this.

The procedure that produced these choices — and the one to apply to a new one —
is [Dependency principles](dependency-principles.md).

## Language: Go (and not TypeScript / Python)

LLMs reach for TypeScript and Python by familiarity, not fitness — the same
flywheel as React: TS grows on React/Next's back, Python on AI's (+7% in 2025).
Both are defensible defaults elsewhere; here they lose on three structural
points.

- **One artifact.** A single ~30 MB binary in a minimal alpine image, against
  a runtime plus `node_modules` plus a build step (Node), or an interpreter plus
  venv plus a packaging matrix (Python).
- **Concurrency by default.** Goroutines against a single-threaded event loop
  (Node), or a GIL that in 2026 means running a second interpreter (3.14t,
  opt-in) whose lock silently re-enables on any unmarked C extension — with no
  stable ABI and no official container tag yet.
- **Checkable output.** Millisecond builds and `gofmt` uniformity. A wide stdlib
  (http, json, testing, pprof in the box) means fewer third-party tickets in the
  supply-chain lottery — Node reaches for express/vitest, Python for
  fastapi/pytest. `go test -race` catches data races with one command; neither
  rival has an equivalent.

Rejected: **Node** (single thread, a build step per deploy, plus an
install-time code-execution primitive — npm `preinstall`/`postinstall` hooks run
on `npm install` before any import, the delivery mechanism behind the 2026 worm
waves in `keyv`, `axios`, 140+ `@mastra/*` packages, and 639 `@antv` versions;
Go has no equivalent hook) and **Python** (packaging converged with `uv` plus
lockfiles in 2026, but the interpreter matrix and the GIL migration remain, and
raw speed is an order of magnitude off).

Cost: a smaller hiring pool than TS, no ML/data ecosystem to speak of, slower
raw single-core than C/Zig/Rust.

Revisit per workload: Python the day the app is ML/data-first, TypeScript the
day the team or the product is JS-only and the npm ecosystem pays the binary's
rent.

## goqite + retry-go (and not Redis)

A queue without a service to run: jobs persist in the SQLite the app already
owns, retries back off with jitter, and one binary stays one binary.

Rejected: Redis-style brokers (a second process to persist, secure, and
operate) and JetStream as a job queue (it is the cross-instance broadcast, not a
work queue with per-job retry semantics).

Cost: single-process throughput — depth and worker latency are bounded by this
box. Revisit when profiles show the queue, not the work, as the bottleneck.

## DagNats (and not Temporal)

Workflows as declarative JSON, embedded in the binary, state durable on the
JetStream the template already runs. No cluster to install, no second surface to
learn.

Rejected: Temporal (a server plus database plus SDKs for durability most apps
never need at this scale).

Cost: bounded dynamism by design — conditions-as-data (`skip_if`), bounded agent
loops, planner fragments under `MaxSteps`/`MaxDepth`/`AllowedTasks` (full table:
[dagnats](dagnats.md)). Revisit when a workflow needs unbounded iteration with
no static bound, free-form expression evaluation the validators reject, dynamic
topology beyond the planner bounds, multi-tenant operation (namespaces, per-team
RBAC and audit), or a hosted offering. Anything below that is a new DagNats
primitive or a worker-side decision, not a new engine.

## JetStream on by default (and not opt-in)

Durable workflows, cross-instance broadcast, and collab sync all assume it, so
on-by-default keeps every feature working out of the box — and one env var
switches it off.

Cost: an embedded NATS server in every boot, its ports and storage in every
deploy. Strictly single-instance with no workflows? `NATS_ENABLED=false` removes
the cost entirely.

## GoAI (and not the provider SDKs)

One injectable interface over any OpenAI-compatible provider, with a stub
(`internal/llm/fakeserver`) that keeps real LLMs out of tests.

Rejected: one SDK per vendor (a dependency each, untestable without network).

Cost: the wrapper lags newest provider features and adds its own surface to
learn. Revisit per provider when it blocks a capability you actually call.

## Genkit (and not zenflow, and not raw goai calls)

Genkit is the typed-flow layer above the injectable client: schemas inferred
from Go structs, progress streaming, per-step OTel spans, and a Dev UI with
evals — with **zero provider plugins**, so generation still reaches providers
only through `llm.Client` and the Biller choke point holds.

Raw goai calls stay right for single-shot generation (Suggest); the flow earns
its ~16 MB when orchestration needs types, traces, and replayable runs.

Rejected: **zenflow** (stale upstream, pre-1.0 API, YAML + coordinator overhead
per run) and **provider plugins / `compat_oai`** (a second provider path around
billing).

Cost: a second mental model plus retry/billing discipline — Genkit retry and
billing middlewares stay off, goai's native retry is the single layer. Revisit
if a second AI feature does not need flows: unused orchestration is dead weight
no matter the megabytes.

## Classifier judge (and not a generative judge)

The review loop's judge is a decision API (Jev-compatible), not a chat
completion: typed choices with confidence and probabilities instead of a prose
verdict that needs parsing — ~100x cheaper per call, and more accurate exactly
where the demo lives (prose; classifier judges trail on math/code/logic, which
is why the code tab is second). Any server speaking `POST {state,model,questions}`
works (TypeSafe, self-hosted SimpleJev, tensai) via `JEV_*` env.

Cost: one more backend to configure and calibrate (~20 hand-labelled cases
before trusting auto-accept). Revisit per workload: a rubric a classifier cannot
express (open-ended reasoning quality) wants a generative judge behind the same
terminal step — the flow does not care which backend answers.

## Generative UI catalog (and not a React GenUI SDK)

The model picks components from a fixed registry — `text_note`, `plan_cards`,
`data_table`, `bar_chart`, `action_buttons`, `diagram` (Mermaid via vendored
Pele, client-rendered SVG with source fallback), nestable `section`s — and the
server renders them as Templ over SSE: no client framework, no JS bundle,
trimmable like every feature.

- **Rejected:** OpenUI/A2UI/AG-UI/CopilotKit renderers (all React/JS with npm +
  SPA; none ships a Go/hypermedia renderer), *unsanitized* free-form model
  HTML (unreviewable, untestable, XSS surface), and the community AG-UI Go
  SDK (version churn for ~10 string constants — mirrored stdlib-only with a
  wire-compatibility test instead).
- **Cost:** the catalog grows by developer PR only; recursion is depth-capped;
  free-model rosters churn monthly (pin and monitor).
- **Tolerance is structural, never catalog.** The one shared parser repairs the
  shape models get wrong — ```json fences, prose padding, trailing commas, a
  repeated envelope key written as an array element, a section's children under
  `components` instead of `children`, an object closed with `]`, an answer cut
  off mid-token — and no more. An unknown type, undecodable props or over-deep
  nesting still rejects the whole answer: a repaired shape can only ever render
  components the registry already knows. A payload that fails validation
  degrades to the prose the model wrote around it, so a chat bubble never
  carries raw JSON.
- **Exception, scoped:** chat's `html_block` lets the model compose visual
  markup the catalog cannot express — but sanitized server-side through a
  closed allowlist (structure + closed class vocabulary + safe links; scripts,
  styles, handlers, media never survive), size-capped, red-proofed. It is
  chat's default answer form: the prompt advertises it first. `GENUI_CHAT_SURFACES=catalog`
  is the strict showcase — unknown types fail closed with one bounded repair.
- **Revisit when:** an external AG-UI client needs the stream, or a renderer the
  model can't express in server-rendered HTML.

## Loro, server-owned (and not Yjs / Automerge / OT)

The merge engine with the smallest client footprint: none. The server owns the
Doc, browsers POST ops and render the JSON broadcast back — no JS CRDT library,
no npm build step — and offline edits still merge without last-write-wins.

Rejected: Yjs in the browser (a second CRDT system doing Loro's job, plus a
bundler, plus a sync protocol that fits websockets better than the SSE + NATS
transport here); Automerge (same overlap, same cost); OT in the style of the
Datastar collab demo (server-serialized, plain text only, no offline merge).

Cost: every keystroke round-trips — no client-local editing — and the rich-text
editor binding (cursor mapping, marks) is future work, as a `feature` package
reusing `internal/collab`, never a new plugin.

## age + `~/.secrets/` (and not Vault / KMS)

Secrets as encrypted files, decrypted at boot: zero services, zero bills, zero
IAM.

Rejected: Vault / cloud KMS (a service to run and pay for, so a single box can
hold its own secrets).

Cost: no rotation story, and distribution across instances is manual. Revisit
when secrets outgrow one box or need audit trails.

## Wails + gogpu/ui (and not Electron / Tauri)

Desktop shells over the same backend: Wails for a webview shell with offline
edge sync, gogpu/ui for a pure-Go window with no webview and no HTTP at all.

Rejected: Electron (a Chromium per app, its own update and CVE surface).

Cost: the platform matrix and native debugging stay yours. Both are
delete-with-the-package features, never core.

## 30+ linters (and not `go vet` alone)

Tuned for agent-written code: unchecked errors, leaked contexts, unclosed
bodies, weak crypto, template-signal mistakes — enforced in pre-commit hooks and
CI, taught upfront via the coding-standards skill.

Cost: CI minutes and a `//nolint` discipline curve. Revisit by deleting rules
that never fire, not by adding more.

## Database: Turso, and the server databases it replaces

The data layer is embedded SQLite at the file level, spoken through
sqlc-generated queries. There is no database server to install, connect to, or
pay for. The choice is really three-way — plain SQLite, **Turso Database** (the
engine the app runs on), or a server database — so each is compared on its own
terms.

### Turso Database (and not plain SQLite, and not Postgres/Supabase/Firebase)

**Turso is not "SQLite".** Turso Database is the ground-up Rust rewrite of
SQLite, driven from Go by `turso.tech/database/tursogo` (purego, no CGO). Same
file format and SQL dialect, so tooling and migrations carry over — but a
different engine underneath, with its own roadmap (MVCC for concurrent writers,
per-tenant databases). The repo also opens *plain* SQLite files (through
`ncruces/go-sqlite3`) on the paths that do not need Turso; see
[Why ncruces?](stack-layers.md#why-ncrucesgo-sqlite3).

So the honest comparison is:

- **vs plain SQLite.** SQLite serializes writers (one writer at a time, with
  `database is locked` retries); Turso keeps the same model today and is the
  engine on the MVCC/multi-writer path. Choosing Turso is choosing the engine
  with the concurrent-write roadmap, at the cost of a pre-1.0 dependency; plain
  SQLite would be the more conservative pick with a shorter runway.
  Status, probed (2026-10): the engine HAS concurrent writes (`BEGIN
  CONCURRENT`, MVCC + group commit, Turso 0.8 benchmarks), but the pinned
  `tursogo` rejects it unless MVCC is enabled — and nothing in this tree
  enables it (WAL alone is not enough; proven reachable on the pin: the
  pragma is accepted and two concurrent writers both commit, probe deleted
  after). So writers still serialize here by configuration, not by engine
  ceiling: `busy_timeout` + short transactions stay the stance until
  something opts in AND adds conflict-retry handling
  (concurrent conflicts error instead of blocking). Runbook for that day:
  `docs/db-concurrency.md` (exact steps as probed, red-proof plan,
  revisit triggers).
- **vs Postgres.** Postgres is the #1 database in the 2025 Stack Overflow survey
  (58%). It genuinely wins when sustained multi-writer contention on the same
  rows, true horizontal writes across regions, or heavy in-app analytics show up.
  Until a profile proves one of those, "only Postgres scales" is premature: a
  single NVMe box runs SQLite/WAL at 100k+ reads/s and ~10k writes/s, readers
  never block the writer, and the first rungs are batching (one transaction, not
  N commits), a generous `busy_timeout`, and a write queue — not a second server.
- **vs Supabase / Firebase.** These move your data behind a network call, a
  bill, and someone else's changelog. Supabase (13M+ developers) bought Turso
  (Oct 2026) with a stated thesis — SQLite to start, Postgres to scale — which
  is also the revisit trigger below.

Rejected: assembling it yourself (router + SQL + session auth + an admin you
write and secure) and outsourcing it (Supabase/Firebase).

#### Turso Database: the cost, stated plainly

`tursogo` is a `database/sql` driver (`sql.Open("turso", path)`) loaded through
purego, so there is **no CGO** and the cgo-free cross-compilation story holds.
Three things are worth knowing before this becomes the only engine:

- **Heavy platform libs.** The module `github.com/tursodatabase/turso-go-platform-libs`
  carries a prebuilt Rust shared library per platform (~15 MB each, ~124 MB of
  module). The build tag selects one — and on musl runtimes that MUST be
  `-tags musl` (libs/linux_*_musl); Go does not set the musl tag automatically,
  so without it the glibc `.so` is embedded and dlopen fails on Alpine. At first
  use the library is written to a cache directory (`TURSO_GO_CACHE_DIR`, else
  `os.UserCacheDir()`), verified against an embedded SHA-256, and `dlopen`ed. A
  container whose cache directory is unwritable needs `TURSO_GO_CACHE_DIR`
  pointed at a writable path.
- **dlopen needs a dynamic loader.** A statically linked binary
  (`-extldflags=-static`) or a `scratch` runtime panics at the first DB open with
  `Dynamic loading not supported` — this crash-looped production on the 0.44.0
  migration. `deploy/Dockerfile.prod` therefore runs on Alpine (musl loader +
  libgcc_s), builds with `CGO_ENABLED=1 -tags musl` and no `-static`, and
  `scripts/smoke-prod-image.sh` boots the final image and asserts `/health` in
  CI. Never move prod back to `scratch` without re-proving the boot.
- **Pre-1.0.** Keep backups (`scripts/backup.sh`) — the driver's own README says
  the same thing.

#### Scaling embedded SQLite — the honest ladder

The stance is single-server vertical: 10k+ persistent realtime connections on a
$4 VPS, a generous `busy_timeout` against a serialized writer with built-in
`database is locked` retries. That covers small to midsize SaaS without trying.
Past that, in order:

1. **Disaster recovery** — Litestream streams WAL to S3-compatible storage.
   Restores in seconds; no app changes.
2. **Read distribution** — libsql embedded replicas serve reads from a local file
   at local-disk speed while writes forward to a primary. Caveat that kills it
   for us today: embedded mode needs CGO, which conflicts with the cgo-free
   `ncruces` stance that keeps cross-compilation clean.
3. **Community HA** — NATS-replicated SQLite (last-writer-wins or static leader),
   libsql primary-replica topologies, Marmot+NATS multi-primary. All third-party,
   all with the same asterisk: the record-event fan-out is evaluated in-process,
   so it does not sync across nodes the way rows do. Leader-for-writes is the
   only consistent topology.
4. **Graduation** — a Postgres migration (the sqlc queries are portable SQL;
   auth, sessions and realtime are already ours, so there is no framework surface
   to replace). That is a project, not a flag flip — which is why the trigger is
   measured write load, not anxiety.

Write-heavy-global (payments, bidding, telemetry at planetary scale) skips the
ladder and starts at Postgres. Everything else should have to prove it needs to
leave.

#### Cloud counterparts, if the box ever stops being enough

Turso (embedded replicas, per-tenant databases, MVCC concurrent writes still in
beta — not a bet this quarter), Cloudflare D1 (zero-config inside Workers, ~10 GB
ceiling, Workers-only), Supabase Postgres (the graduation destination, now with
Turso inside it). None of them change the default: the app runs whole on one box
with zero cloud dependencies until the ladder above says otherwise.

## Routers: chi for ergonomics, not speed

The stack composes one `chi.Router` and flattens each capability's own router
onto it (`MountFeature` in `internal/server/route.go`). chi is the choice for
its ergonomics — a stdlib-shaped API with a middleware chain, URL params and a
router-level 405 — and for its zero-dependency footprint.

The famous alternatives beat it on microbenchmarks:

- **Gin** (89k stars) — zero-alloc radix, custom context, 12 deps.
- **Echo** (33k stars) — JSON/p99 balance, 8 deps.
- **Chi** (22k stars) — zero deps, stdlib-pure; the one Supabase's own auth
  service uses.

None of that is why chi is here. Routing is microseconds against milliseconds
of SQLite, and the SQLite wisdom holds exactly — ignore artificial benchmarks,
apps bottleneck on DB operations. A router choice is an ergonomics decision;
there is no router upgrade that moves our numbers. The day routing shows up in
a profile, the database has already been the problem for months.

## Datastar (and not htmx, and not a JS framework)

Datastar's own guide puts it plainly: backend reactivity like htmx plus frontend
reactivity like Alpine, with no npm packages. Signals hold state, the backend
drives DOM patches, and SSE streaming is a first-class primitive — unlike
either. htmx overlap is real and acknowledged: simple hyperlink-style swaps stay
simpler in htmx.

Rejected: SPA frameworks (a bundler, hydration, a second source of truth in the
client) for the default path.

Cost: a younger, smaller ecosystem than React; persistent connections your
proxies must allow; more logic lives in the backend by design. The repo pays
down the newness with `datastar-lint` and the `internal/datastar` wrapper.

### React/Next: the flywheel is real, and so are its costs

State of React 2025 names the mechanism honestly: React's ecosystem mass means
more training data, which makes AI better at React, which brings more
developers, which makes more data (React ~47%, Next.js ~21% in the 2025 Stack
Overflow survey). An LLM reaching for Next.js is usually reaching for
familiarity, not fitness — which is exactly why it should not decide the stack.

The costs are structural, converging across independent 2026 production reports:

- a misplaced `use client` pulls its whole subtree into the client bundle;
- server/client boundaries multiply cache, transport, and debugging surface
  (origin CPU per request, Flight payloads, hydration roots);
- the apps that pay most — dashboards, admin panels, realtime collaboration, the
  exact shape of this template's features — gain least, because interactivity
  everywhere collapses the server-first advantage while keeping its overhead.

Datastar is 11.29 KiB full (~5 KiB core): signals, computed, effects, and SSE
`patchElements`/`patchSignals` — everything realtime needs, no npm, no build
step, no second source of truth to drift.

## Demo features shipped (and not a bare core)

`todo`, `whiteboard`, `room`, `notes` ship as working pages — not because every
app needs a todo list, but because patterns teach better than prose: todo is the
reference MVC, whiteboard the CRDT canvas, room the actor pattern, notes the
collaborative text. The AI demo follows the same rule: Chat proves constrained
generation, chat proves persisted history, review proves a typed multi-role loop
with a classifier judge.

Rejected: a minimal core with nothing to copy. "Patterns teach better than prose"
is a claim about taste until it is tied to the machinery, and three specifics tie
it:

- **The demos are the conformance suite of the core seams.** When the framework
  layer left, every contract its tests pinned was re-pinned on the slice that
  replaced it — roughly twenty of them for `notes` alone. A bare core has no
  contract to re-pin, so a change to a seam breaks wiring silently instead of
  failing a named test.
- **The installer works on wiring that only exists in the demos.** `trim`, `add`
  and `new` edit markers in real files — the call lines in `cmd/web/main.go`, the
  route lines in `internal/server/chi.go`, the module registry entry, the `// SCOPE:`
  annotations. With no demo in the tree, `--features none` has nothing to strip
  and `gogogo add` has nothing to restore, so "ship everything, delete what you
  do not want" loses its input.
- **The gates walk units that exist.** `registry-conformance`, `check-scope` and
  the RBAC coverage scan enumerate real units, so fewer units means less surface
  under a gate — which is how a route ships unguarded.

Cost: scaffold weight and trim burden, and the weight is measurable. Trimming the
four demo features and their wiring removes 81 of the 736 code and docs files,
about 15,000 lines. Trimming every unit the installer offers removes 218 of them,
about 39,700 lines, near a third of the tree. Counted on a copy of the tree with
generated bundles excluded. Each demo is a unit the installer
offers to strip (`whiteboard`, `goakt`, `landing`, `config-view`, `credits`,
`sounds`, `review`, `genui`, `dagnats`) or a documented manual removal
(`todo`, `notes`), and every demo rots if its pattern drifts from the core.
Revisit per demo: anything nobody copies in real scaffolds is a deletion
candidate, not heritage.

## The flat stack — the cost

[Architecture decision](architecture-decision.md) argues the benefits. The cost,
stated plainly: we own every line above (auth, schema, realtime fan-out), Turso
is pre-1.0 with heavyweight platform libs, and there is no admin UI, generic REST,
or file storage to lean on.

Revisit if owning the commodity parts costs more than the opinions they replaced —
the day the team wants a database GUI, a generic REST surface, or file uploads
without writing them.

**Rejected: PocketBase** (`github.com/pocketbase/pocketbase`), the embedded
all-in-one this template ran before 0.44.0. It gives a lot at once — an admin UI,
auth, file storage, generic REST, realtime subscriptions — and it is rejected on
a different axis: it is a framework SURFACE, not a database choice. Those
surfaces are the seams this template owns (chi composition, argon2id sessions,
data access, fan-out), so adopting it puts the app inside its lifecycle and its
hooks, leaves no unit to trim and no line you own. Swapping the store keeps every
seam; adopting the framework deletes them.

What refusing it costs is the list above: no admin UI, no generic REST, no file
storage, with data read by any SQLite client against the file and the schema held
in SQL migrations ([Admin & Dashboard](admin-dashboard.md) is the operator view
that does ship). What the refusal bought is measurable: the binary went from 141M
to 110M, and the app handle, the seeds, the idempotency hook, the middleware and
the `ENTITY_STORE` switch left with it. The ban is mechanical rather than
advisory, `cmd/check-flatdeps` refuses `github.com/pocketbase/` by module prefix,
so it survives an edit to this page.

## Runtime dashboard: statsviz (and not monigo)

Opt-in (`PERF_ENABLED`, dev on / prod off) and net/http-only: statsviz plots
`runtime/metrics` on the chi tree, adding one small dependency
(`gorilla/websocket`) and no second HTTP stack.

Rejected: **monigo**. Its dashboard is net/http, but the package imports Fiber
v2/fasthttp unconditionally (`GetFiberHandler`), so the module graph grows a
second HTTP framework even when only the net/http handlers are mounted.

It also registers its own SIGINT/SIGTERM in `Start()` and buys function-level
tracing plus a time-series store — more than this surface needs. **pprof** is
stdlib and rides the same fence; it supplies the depth (CPU, heap, goroutine,
trace) without a dependency.

Cost: no persistence and no function tracing ("what is the process doing now",
not "what did this call cost last hour"). Revisit with Pyroscope or
Prometheus/OTLP when per-function profiles over time become the question.

## Related

- [Stack in layers](stack-layers.md) — what is in the box and what it buys.
- [Architecture decision](architecture-decision.md) — the flat-stack decision
  and its evidence.
