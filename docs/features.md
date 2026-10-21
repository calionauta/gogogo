# Features

Every capability is always compiled. The headline is the **feature**; the
second column names the **technology** that powers it. The removal class of
whatever sits behind a row (core / plugin / feature) lives in
[Scope taxonomy](scope-taxonomy.md) — the registry is the source of truth, and
a bracketed `(plugin)` marks a technology that serves a feature rather than
being one.

| Feature | Powered by | Runtime opt-out |
|---|---|---|
| **Auth** | Owned argon2id + sessions | — (middleware stays; login UI removable) |
| **Access control (RBAC)** | Casbin v3 + `users.role` | — (middleware always wired; `RBAC_ENABLED=true` enforces) |
| **Todo app + realtime** | SSE Hub record-sync + ephemeral signals | — |
| **Background jobs + retry** | `goqite` + `retry-go` | — |
| **AI Suggest** | GoAI | `GOAI_API_KEY` unset |
| **Ask AI (generative UI)** | Catalog + SSE morphs, no client JS | `GOAI_API_KEY` unset (`SIMULATE_LLM` demo answers keyless), or `trim genui` (bundle with Chat) |
| **Chat (conversation)** | Invented visuals (sanitized) + catalog fallback + persisted history + the app's own panels | `GOAI_API_KEY` unset (`SIMULATE_LLM` demo answers keyless), or `trim genui` (bundle with Ask AI) |
| **Review loop** | Genkit flows (plugin) + GoAI + jev classifier judge | `GOAI_API_KEY` unset (real-LLM only; page shows a setup hint), or `trim review` |
| **AI credits + BYOK** | [ai-credits](https://github.com/calionauta/ai-credits) | `CREDITS_ENABLED=false` |
| **Search (one seam, three layers)** | Own layers — no external engine | `trim search` |
| **Collaborative whiteboard** | Loro CRDT + Rough.js + NATS | `trim whiteboard` (bundle with notes + collab transport), or `COLLAB_SNAPSHOTS=memory` for snapshot-free local mode |
| **Shared notes** | Loro Text (server-owned) + SSE Hub | `trim whiteboard` (bundle with whiteboard + collab transport) |
| **Durable workflows** | DagNats over JetStream | `DAGNATS_ENABLED=false` |
| **Room presence (entity actors)** | GoAkt grains (standalone) | `GOAKT_ENABLED=false` |
| **Multi-instance realtime** | NATS JetStream | `NATS_ENABLED=false` |
| **Hybrid offline sync** | Service Worker + NATS Leaf Node + idempotency | `OFFLINE_SYNC_ENABLED=false` |
| **UI skins (pluggable)** | DaisyUI v5 + BasecoatUI | `UI_SKIN` |
| **Persistence** | Turso (sqlc) | — |
| **Desktop-edge sync** | NATS Leaf Node | `NATS_LEAFNODE_URL` unset |
| **Landing page** | Templ | — |
| **Read-only config view** | Templ | — |

## What each capability does

- **Auth** — Cookie sessions, demo login, per-request user loading.
- **Access control (RBAC)** — Roles on the user row (`admin`, `user`, `viewer`, `anonymous`) with the policy in one Casbin model, enforced before the handler runs. Route+verb in policy; per-record ownership in `canDeleteDoc`/`canDeleteRow`. Embedded policy is the default; `RBAC_POLICY_SOURCE=db` moves it to `casbin_rule`. Coverage is checked by scanning every router, so a new route cannot ship unguarded. Demo accounts ship as admin/viewer. See [RBAC](rbac.md).
- **Todo app + realtime** — DB actions (create/toggle/delete) stream visibility-scoped via `owner` + `visibility` (private rows to owner tabs, shared rows to every login). Ephemeral signals carry toasts, clients count, AI suggest.
- **Background jobs + retry** — Background jobs with backoff/jitter. Stepper UI streamed via SSE (`techStep` / `techPhase`).
- **AI Suggest** — LLM call from the todo UI; button hidden when no key. Stepper signals `aiStep` / `aiPhase`. Proves the queue+retry path against a real provider, including Biller metering when wired.
- **Ask AI (generative UI)** — Model picks cards, tables, sections from a fixed registry (plus the app's own native panels, which are always advertised on BOTH AI legs — an answer about the user's own data comes from the app's data); unknown types fail closed; one bounded repair attempt feeds a rejected answer's validation error back to the model before the failure reaches the user; worker streams the render over a dedicated SSE hub (`/genui`). Optional `JEV_ROUTER_ENABLED=true` lets a jev-compatible classifier pick which catalog component the answer leads with (off by default; any failure falls back to the full catalog). Proves catalog-constrained generation (the model cannot emit arbitrary HTML) with unknown-type fail-closed.
- **Chat (conversation)** — Conversation where the answer is invented for the request: the model composes Tailwind/DaisyUI markup (`html_block`, advertised first in the prompt), sanitized server-side through a closed allowlist, and limited to a safelisted class vocabulary (anything outside it renders unstyled, so the prompt names the exact list), with the catalog kept for what markup cannot do — so chat does not read like Ask; and `todo_list {filter}` for questions about the user's OWN todos, whose rows the worker hydrates from the store (the model picks the slice, never the content, and gets a bounded digest of the REAL rows in its prompt so a question about them is answered instead of invented; a panel that cannot be hydrated is replaced by an honest "couldn't load this" placeholder with a link, never dropped and never shown empty); prompts persist per owner and history is server-rendered on load, answers render inline as text plus components patched into the live column; a payload that fails validation degrades to the prose around it, never to raw JSON in the bubble; dedicated SSE hub (`/chat`). Proves persisted per-owner history with invented-but-safe visuals.
- **Review loop** — Writer → critic (bounded) → classifier judge, streamed verdict, discarded. Zero server-side artifacts (billing ledger only). Any jev-compatible judge via `JEV_*` env — see [Review loop](review.md). Proves a typed multi-role loop with a classifier judge, per-role cost attribution, and a confidence cascade to human approval.
- **AI credits + BYOK** — Optional plugin: meter Todo AI Suggest with reserve/settle, expose balances/top-ups, and proxy a user's encrypted provider key through an OpenAI-compatible BYOK relay.
- **Search (one seam, three layers)** — One `Searcher` seam, three layers **selectable at runtime** from the search box (`Match`: exact / typo-tolerant / relevance) with `SEARCH_MODE` choosing the default a tab starts on: `substring` (default; SQL `LIKE` over the rows, no index, no extra write), `fuzzy` (in-memory tiers, typo tolerant: 1.00 verbatim → 0.95 all-tokens → 0.90 prefix → 0.80 one-edit → 0.75 subsequence), and `bm25` (an inverted index in the same DB, scored in Go: a rare term outranks a common one and a short doc outranks a long one repeating the term; always available, and a boot-time heal re-indexes rows that predate the index so enabling it never answers "no results" for data you can see). Search only ever NARROWS the caller's visible set — a hit whose row is not visible is dropped, never resurrected. In `bm25` the postings ride the row's own transaction, so no row exists without its index entry. No external engine because this driver ships no FTS5 (verified across tursogo v0.8.2/v0.8.3/v0.9.0-pre.1) and a second index store would put a row's truth in two places.
- **Collaborative whiteboard** — Canvas, SSE + NATS broadcast, **live in-progress drawing** (the shape under the pointer is relayed on the volatile presence channel so peers watch it grow, then committed by the `add` op on release), offline-first outbox replay, Turso-persisted snapshots, server-stamped cursor identity (roster and cursors kept in separate maps; idle cursors expire on an 8 s TTL). Boards are **shared**: the index lists every board with its author, every signed-in user opens and draws on any of them, and deleting is author-or-admin (`canDeleteDoc`).
- **Shared notes** — Plain-text collab notes: character ops merge server-side, resolved text streams to peers, presence + typing + line carets (each caret measured against the reporter's own text snapshot, so a dot never lands on the wrong line while the two editors are briefly diverged; only a FOCUSED textarea ever reports a caret — an idle tab that adopted a peer's text owns no position to announce, and the 5s heartbeat resends the last own caret instead of reading a borrowed selection; a closed tab announces `/leave` from `pagehide` instead of waiting out the 8s expiry), live index via the Hub record event + `ListResync` (no stream on the index page), snapshots in the `notes` table. Notes are **shared**: the index lists every note with its author, every signed-in user edits any of them, and deleting is author-or-admin (`canDeleteDoc`), which drops the metadata row and the snapshot together.
- **Durable workflows** — JSON workflows — HTTP API on `:8090`, durable state on `:4222` (e.g. `WelcomeOnboarding`).
- **Room presence (entity actors)** — One grain per room: connection-scoped roster (SSE join/leave, refcounted per tab) + exactly-one presenter lock, supervised with restart budget. The roster is pushed on every change and name-ordered (a map range shuffled it between pushes), the tab keeps its stream while hidden (`openWhenHidden`, or a tab switch would read as leaving), a crash restart replays the live connections into the fresh grain, re-acquiring your own lock succeeds, and the page renders only the actions the lock actually allows — Take while it is free, Release for the holder. Demo at `/room/` with a crash hook.
- **Multi-instance realtime** — JetStream fan-out for todo + whiteboard sync across >1 instance behind a LB.
- **Hybrid offline sync** — NATS CRUD proxy + Service Worker offline queue (default on). Desktop edges publish CRUD ops via JetStream; web clients use Service Worker + Background Sync.
- **UI skins (pluggable)** — DaisyUI (default) or BasecoatUI (shadcn-style OKLCH tokens). Switch via env var or `?skin=`. See [UI skins](ui-skins.md).
- **Persistence** — One data layer: `internal/db` migrations + generated queries, no strategy switch.
- **Desktop-edge sync** — Leaf-Node JetStream replication of Loro updates for desktop/edge clients.
- **Landing page** — The app's own public page on `GET /` (not the GitHub Pages promo site, which is never installed). No auth, no DB. Todo demo lives at `/todo`.
- **Read-only config view** — Auth-gated `GET /config`: env-decrypted values, masked secrets, runtime constants. Never mutates state.

## Adding a new feature


0. Scaffold it: `go run ./cmd/gogogo new feature <name>` writes the handler +
   page, wires `BootChi`/`buildChiMux`, registers the capability unit, adds the
   RBAC rows, regenerates the docs and proves the build. Read the unit's card in
   [Unit contracts](unit-contracts.md) first — it lists the routes and generated
   queries the unit already owns.
1. Create `features/<name>/` with its HTTP handlers + Templ components.
2. Wire it in `internal/server/chi.go` — build the handler in `BootChi`,
   mount the routes in `buildChiMux`.
3. Use **goqite** for async work, the **SSE Hub** for user-facing feedback, and
   **Datastar** for the reactive frontend.
4. Add a `SCOPE:` annotation — `make check-scope` enforces it.
5. Add a `Routes()` method on your handler and mount it in `buildChiMux`.

A feature never imports another feature. Three shared UI leaves are the only
exception (`features/authviews`, `features/sounds`, `features/genui/components`);
`cmd/check-flatdeps` fails the gate on anything else, so a trim never breaks a
neighbour.

The [Todo feature](todo-example.md) is the full reference implementation.

## The surfaces you get

| Surface | URL | Notes |
|---|---|---|
| Landing page | `/` | Public. Same page for guests and signed-in users |
| Todo demo | `/todo` | Auth-gated. Seeded demo account |
| Ask AI demo | `/genui` | Auth-gated. Keyless demo answers; live model with `GOAI_API_KEY` |
| Chat demo | `/chat` | Auth-gated. Persisted per-owner history; model-invented markup (sanitized) with catalog fallback |
| Review loop | `/review` | Auth-gated. Writer → critic → classifier judge, streamed verdict, discarded. Runs on the `genkit` plugin |
| Whiteboard | `/whiteboard` | Auth-gated |
| Room demo | `/room` | Auth-gated. Heartbeat roster + presenter lock owned by a GoAkt grain |
| Config view | `/config` | Auth-gated, read-only |
| DagNats console | `:8090` / `/dagnats/` | Workflow runs and steps |

Details in [Admin & Dashboard](admin-dashboard.md).

## Ask AI vs Chat: two modes, one surface vocabulary

The two AI legs answer with the same kinds of component. What still
separates them is the interaction shape, not the vocabulary:

| | Ask AI | Chat |
|---|---|---|
| Shape | one-shot: one request, one answer region | conversation: turns persisted per owner, history server-rendered |
| A bad answer | loud: the region says it could not shape one | degrades to the prose around it |
| Follow-up | none (a new request replaces the answer) | the previous turns are context |
| Surfaces | the same three bands, minus `custom` by default | all three |

**The three bands, split by who owns the content:**

- **`catalog`** — the fixed generative registry. The model picks a
  component name and fills typed props. Static: nothing depends on app
  state.
- **`custom`** — `html_block`. The model composes markup for this
  request; sanitized server-side through a closed element allowlist and a
  safelisted class vocabulary. This is the only band whose content the
  model invents wholesale, and it is what makes Chat read unlike Ask.
- **`native`** — the app's own panels (`todo_list {filter}`). The APP owns
  the content (real rows from the store); the model picks only the slice.
  Always on, on BOTH legs: an answer about the user's own data should come
  from the app's data, and there is no reason to want a worse answer.

**Actions are declared by features, never written by the model.** A catalog
component can offer a write (`plan_cards`' per-plan button,
`action_buttons`), but the URL, method and field NAMES must match an action
a feature declared (`ActionProvider.Actions`) — the model chooses *which
offered action, with what content*, never *which endpoint*. That closes both
failure modes at once: told nothing it invents `/api/todos/add` and the
button 404s; told everything it could aim a card at any same-origin route.
The vocabulary it may use appears in the prompt because boot advertises the
declarations.

**Every rendered component carries a server-assigned instance id**, and that
id is the component's patch slot. The model cannot set it (it is stripped
before validation — an id becomes a selector, so a model-chosen one could
aim a patch at any element), and it is namespaced per answer, so a page
holding two answers (a chat thread keeps its history) has no duplicate ids.
An action form sends `_target` with its own slot, which is how a write
answers **in place**: the control the user clicked is what changes, instead
of a patch aimed at a region this page does not host — the difference
between "it worked" and "I clicked and nothing happened".

A panel whose data cannot be read is replaced by a placeholder that keeps
its slot id, names the panel, and links to the page the PROVIDER says owns
the data (no shared component hardcodes a feature's route). A write that
fails does the same in place, because a plain-text error response renders
nowhere when the form was submitted by `@post` from a card.

Tune a leg's bands with `GENUI_ASK_SURFACES` / `GENUI_CHAT_SURFACES`
(CSV; absent = the defaults above; an unknown name fails the boot).
Turning `custom` off makes a leg catalog-only; `native` cannot be turned
off. `JEV_ROUTER_ENABLED=true` lets a jev-compatible classifier pick which
component an answer LEADS with — a hint, never a narrowing — and now
serves both legs.

## Removing a feature

Every feature package carries a `SCOPE:feature` annotation. The rule:
delete the package directory, delete its dependents, remove the wiring call
from `internal/server/chi.go`, and — if it was a plugin — remove the
`start*` call in `cmd/web/main.go`. Full checklist in
[Scope taxonomy](scope-taxonomy.md#removing-a-component).

## Client weight

Vendored JS per feature, measured deterministically (`wc -c` raw,
`gzip -nc` for wire; rounded up). CSS is one shared bundle and is not
attributed per feature.

| Feature | Files | Raw | Gzip |
|---|---|---|---|
| Todo, Room, Landing, Config, Ask AI, Chat | inline only (no extra JS) | 0 KB | 0 KB |
| Shared notes | `notes.js` | 28 KB | 10 KB |
| Whiteboard | `whiteboard.js` + `rough.min.js` | 56 KB | 19 KB |
| Shared core (every page) | `datastar.js` + `theme.js` | 40 KB | 16 KB |

Whiteboard and notes pages additionally load the shared
`throttle.js` (~2 KB raw, ~1 KB gzip) — leading + trailing throttle
for cursor/caret reports, one implementation for both features.

Split rule (Datastar vs vanilla, enforced by review): list fragments
and UI state that patch cleanly go through the Datastar runtime (todo,
whiteboard list, notes index — every one of those pages loads
`datastar.js`, pinned by test); canvas rendering, textarea math, and
op batching stay vanilla JS (no declarative equivalent, and the offline
outbox needs exact control). Shared chrome counts as UI state: the
navbar logout form carries `data-on` handlers, so every page rendering
the navbar loads the runtime too (board, notes doc, room, config,
landing) — a `data-on`/`data-bind` attribute on a page without the
runtime is dead markup, and here it would silently skip the logout
service-worker cleanup.

Session visibility uses the stream a page ALREADY has: notes/whiteboard send
a `collab.SessionEvent` frame, and todo/room/chat/genui carry a
`sessionAuthed` signal on their feature stream (revalidated on its heartbeat).
Only a page with no feature stream opens `/api/session/stream` (one visible tab
per browser via Web Locks, BroadcastChannel for the others). One long-lived
connection per page is the rule: browsers cap connections per host, and a
second stream per page has been measured to starve a later same-tab
navigation (the session stream opening on top of a feature stream made the
smoke's seventh page hang on `load`). No tab schedules a request, so every tab
raises the same `components.SessionBanner` the moment a cookie expires. The
banner never auto-redirects (unsent work would be stranded). It is opt-in per
page via the render-time `authed` flag, so a public page never nags about a
session it never had.

Method: `for f in <files>; do wc -c < $f; gzip -nc $f | wc -c; done`.
`datastar.js` is 34 KB raw / ~13 KB gzip — that is the "~12 KiB client"
in the README (wire size). Icons (`iconify`, 24 KB raw) load only on
pages that use them; sounds (`cuelume.js`, 10 KB) only with the sounds
plugin.

## Related

- [Seven async layers](async-layers.md) — the topology behind these capabilities.
- [The Todo example](todo-example.md) — the pattern to imitate.
- [Desktop & Mobile](desktop-mobile.md) — two more frontends over the same backend.