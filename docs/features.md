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
| **Security audit trail** | `security_events` (Turso/sqlc) + `/config` panel + CSV | — (core; 30-day retention) |
| **Todo app + realtime** | SSE Hub record-sync + ephemeral signals | — |
| **Background jobs + retry** | `goqite` + `retry-go` | — |
| **AI Suggest** | GoAI | `GOAI_API_KEY` unset |
| **Chat (conversation)** | Invented visuals (sanitized) + catalog + diagrams + persisted history + the app's own panels | `GOAI_API_KEY` unset (`SIMULATE_LLM` demo answers keyless), or `trim genui` (removes Chat) |
| **Review loop** | Genkit flows (plugin) + GoAI + jev classifier judge | `GOAI_API_KEY` unset (real-LLM only; page shows a setup hint), or `trim review` |
| **AI credits + BYOK** | [ai-credits](https://github.com/calionauta/ai-credits) | `CREDITS_ENABLED=false` |
| **Search (one seam, three layers)** | Own layers — no external engine | `trim search` |
| **Collaborative whiteboard** | Loro CRDT + Rough.js + NATS | `trim whiteboard` (bundle with notes + collab transport), or `COLLAB_SNAPSHOTS=memory` for snapshot-free local mode |
| **Shared notes** | Loro Text (server-owned) + SSE Hub | `trim whiteboard` (bundle with whiteboard + collab transport) |
| **Durable workflows** | DagNats over JetStream | `DAGNATS_ENABLED=false` |
| **Inbox + reminders** | DagNats `sleep` → owner-scoped delivery row | `DAGNATS_ENABLED=false` hides the form; the inbox stays |
| **Entity actors** | GoAkt grains (room lease + whiteboard presence) | `GOAKT_ENABLED=false` (whiteboard falls back to `PeerSet`) |
| **Multi-instance realtime** | NATS JetStream | `NATS_ENABLED=false` |
| **Durable offline writes** | page outbox (write-ahead + receipts) + app-shell cache + NATS Leaf Node | `OFFLINE_SYNC_ENABLED=false` (cache + CRUD proxy only) |
| **UI shell + themes** | DaisyUI v5, four palettes | — |
| **Persistence** | Turso (sqlc) | — |
| **Desktop-edge sync** | NATS Leaf Node | `NATS_LEAFNODE_URL` unset |
| **Landing page** | Templ | — |
| **Config + admin users** | Templ | — |

## What each capability does

- **Auth** — Cookie sessions, demo login, per-request user loading, and the email magic-link / TOTP / password-reset / verification flows on `/login`.
- **Access control (RBAC)** — Roles on the user row (`admin`, `user`, `viewer`, `anonymous`) with the policy in one Casbin model, enforced before the handler runs. Route+verb in policy; per-record ownership in `canDeleteDoc`/`canDeleteRow`. Embedded policy is the default; `RBAC_POLICY_SOURCE=db` moves it to `casbin_rule`. Coverage is checked by scanning every router, so a new route cannot ship unguarded. Demo accounts ship as admin/viewer. See [RBAC](rbac.md).
- **Security audit trail** — Rejections and successful auth, credential changes, session revocation and admin mutations land in `security_events` (subject + actor, never a password/token/code). `/config` shows 24-hour aggregates, filters and pages the latest rows, and exports a filtered CSV; retention is 30 days, pruned at boot and daily in 1000-row batches. Browser CSP violations post to `/api/security/csp-report`. See [Security model](security.md) and [Admin & Dashboard](admin-dashboard.md).
- **Todo app + realtime** — DB actions (create/toggle/delete) stream visibility-scoped via `owner` + `visibility` (private rows to owner tabs, shared rows to every login). Ephemeral signals carry toasts, clients count, AI suggest.
- **Background jobs + retry** — Background jobs with backoff/jitter. Stepper UI streamed via SSE (`techStep` / `techPhase`).
- **AI Suggest** — LLM call from the todo UI; button hidden when no key. Stepper signals `aiStep` / `aiPhase`. Proves the queue+retry path against a real provider, including Biller metering when wired.
- **Chat (conversation)** — The AI demo. Conversation where the answer is invented for the request: the model composes Tailwind/DaisyUI markup (`html_block`, advertised first in the prompt), sanitized server-side through a closed allowlist, and limited to a safelisted class vocabulary (anything outside it renders unstyled, so the prompt names the exact list), with the catalog kept for what markup cannot do — including `diagram` (Mermaid via vendored [Pele](https://pele.run); a ```mermaid fence anywhere in the answer renders as a diagram even when the JSON envelope failed); and `todo_list {filter}` for questions about the user's OWN todos, whose rows the worker hydrates from the store (the model picks the slice, never the content, and gets a bounded digest of the REAL rows in its prompt so a question about them is answered instead of invented; the slice defaults to `open` when the model omits it, because a todo question is about what is left and an omitted filter used to answer "my open todos" with completed rows mixed in; hydrated rows survive the queue/SSE JSON hop, so the live bubble shows them without a reload; a panel that cannot be hydrated is replaced by an honest "couldn't load this" placeholder with a link, never dropped and never shown empty); panels are interactive — the panel renders the SAME `TodoItem` rows and the SAME `deleteConfirmModal` as `/todo`, with only the two write expressions scoped to the panel (slot id `_target` + slice), so the shared badge, author, age, delete gate and confirmation copy cannot drift between surfaces; its Open/All/Done tabs ask the list route with the slot id, so a wrong model pick is one click from the right rows; actions are answered in place by re-rendering that panel, so acting never leaves a stale list, and adding stays on the Todos page. A reload re-fills panels from the store, so a mutation from another tab shows up then. Prompts persist per owner and history is server-rendered on load, answers render inline as text plus components patched into the live column; a payload that fails validation degrades to the prose around it, never to raw JSON in the bubble; dedicated SSE hub (`/chat`). Proves persisted per-owner history with invented-but-safe visuals.
- **Review loop** — Writer → critic (bounded) → classifier judge, streamed verdict, discarded. Zero server-side artifacts (billing ledger only). Any jev-compatible judge via `JEV_*` env — see [Review loop](review.md). Proves a typed multi-role loop with a classifier judge, per-role cost attribution, and a confidence cascade to human approval.
- **AI credits + BYOK** — Optional plugin: meter Todo AI Suggest with reserve/settle, expose balances/top-ups, and proxy a user's encrypted provider key through an OpenAI-compatible BYOK relay.
- **Search (one seam, three layers)** — One `Searcher` seam, three layers **selectable at runtime** from the search box (`Match`: exact / typo-tolerant / relevance) with `SEARCH_MODE` choosing the default a tab starts on: `substring` (default; SQL `LIKE` over the rows, no index, no extra write), `fuzzy` (in-memory tiers, typo tolerant: 1.00 verbatim → 0.95 all-tokens → 0.90 prefix → 0.80 one-edit → 0.75 subsequence), and `bm25` (an inverted index in the same DB, scored in Go: a rare term outranks a common one and a short doc outranks a long one repeating the term; always available, and a boot-time heal re-indexes rows that predate the index so enabling it never answers "no results" for data you can see). Search only ever NARROWS the caller's visible set — a hit whose row is not visible is dropped, never resurrected. In `bm25` the postings ride the row's own transaction, so no row exists without its index entry. No external engine because this driver ships no FTS5 (verified across tursogo v0.8.2/v0.8.3/v0.9.0-pre.1) and a second index store would put a row's truth in two places. Each layer's cost, contract and degrade path: [Search](search.md).
- **Collaborative whiteboard** — Canvas, SSE + NATS broadcast, **live in-progress drawing** (the shape under the pointer is relayed on the volatile presence channel so peers watch it grow, then committed by the `add` op on release), offline-capable outbox replay, Turso-persisted snapshots, server-stamped cursor identity (roster and cursors kept in separate maps; idle cursors expire on an 8 s TTL). Boards are **shared**: the index lists every board with its author, every signed-in user opens and draws on any of them, and deleting is author-or-admin (`canDeleteDoc`). Presence is actor-backed when GoAkt runs (one grain per doc; `PeerSet` fallback when `GOAKT_ENABLED=false`).
- **Shared notes** — Plain-text collab notes: character ops merge server-side, resolved text streams to peers, presence + typing + line carets (each caret measured against the reporter's own text snapshot, so a dot never lands on the wrong line while the two editors are briefly diverged; the offset is measured with a Range over a mirror that holds the reporter's whole text at our font and width — the old zero-width-space marker counted as a character and sat on the PREVIOUS line at a soft wrap, so the dot jumped a row once per typed line; only a FOCUSED textarea ever reports a caret — an idle tab that adopted a peer's text owns no position to announce — and a standalone report is withheld while `ta.value !== serverText`, because it travels without its text and the op batch already carries the exact caret with it; the 5s heartbeat resends the last own caret instead of reading a borrowed selection; a closed tab announces `/leave` from `pagehide` instead of waiting out the 8s expiry), live index via the Hub record event + `ListResync` (no stream on the index page), snapshots in the `notes` table. Notes are **shared**: the index lists every note with its author, every signed-in user edits any of them, and deleting is author-or-admin (`canDeleteDoc`), which drops the metadata row and the snapshot together.
- **Durable workflows** — JSON workflows — HTTP API on `:8090`, durable state on `:4222` (e.g. `WelcomeOnboarding`).
- **Inbox + reminders** — `/notifications` lists delivery rows and schedules a reminder. The delay is a DagNats `sleep` step resolved from `due_at` (RFC3339), so it survives a restart; the delivery row is deduped by `(owner, transaction_id, channel)` and keeps `status` + `skip_reason`, so a delivery that did not happen is a record. An SSE stream patches the inbox live, the rail's inbox icon carries the unread count, and the onboarding workflow delivers through the same path. See [Notifications](notifications.md).
- **Room presence (entity actors)** — One grain per room: connection-scoped roster (SSE join/leave, refcounted per tab) + exactly-one presenter lock, supervised with restart budget. The roster is pushed on every change and name-ordered (a map range shuffled it between pushes), the tab keeps its stream while hidden (`openWhenHidden`, or a tab switch would read as leaving), a crash restart replays the live connections into the fresh grain, re-acquiring your own lock succeeds, and the page renders only the actions the lock actually allows — Take while it is free, Release for the holder. Demo at `/room/` with a crash hook.
- **Multi-instance realtime** — JetStream fan-out for todo + whiteboard sync across >1 instance behind a LB.
- **Durable offline writes** — one page-side outbox (write-ahead + idempotency receipts) plus an app-shell cache; NATS CRUD proxy for desktop edges (default on).
- **UI shell + themes** — every page renders inside one shell (`internal/ui`); the palettes (`light`, `dark`, `lofi`, `dim`) are DaisyUI themes chosen from the account panel in the sidebar and catalogued in `internal/themes`. On phones (≤720px) a bottom dock (Todo/Board/Chat/Menu, DaisyUI `dock`) gives one-tap navigation on every module, including Chat, where the composer sits above the bar. See [UI themes](ui-themes.md).
- **Persistence** — One schema/data layer: `internal/db` migrations + generated queries; the driver is platform-selected: tursogo on web/desktop, `ncruces/go-sqlite3` on Android; `cmd/gui` uses ncruces explicitly over the same schema.
- **Desktop-edge sync** — Leaf-Node live event sync (CRUD + Loro) for desktop/edge clients; offline CRUD replay on reconnect is a follow-up.
- **Landing page** — The app's own public page on `GET /` (not the GitHub Pages promo site, which is never installed). No auth, no DB. Todo demo lives at `/todo`.
- **Config + admin users** — Admin-only `GET /config` (always on, even with RBAC off): env-decrypted values, masked secrets, runtime constants, user role changes, session revocation, the audit panel and CSV. Role/session mutations require the admin's password (or a current code on a passwordless admin).

## Adding a new feature


0. Scaffold it: `go run ./cmd/gogogo new feature <name>` writes the handler +
   page, wires `BootChi`/`buildChiMux`, registers the capability unit, adds the
   RBAC rows, regenerates the docs and proves the build. Read the unit's card in
   [Unit contracts](unit-contracts.md) first — it lists the routes and generated
   queries the unit already owns.
1. Create `features/<name>/` with its HTTP handlers + Templ components.
2. Wire it in `internal/server/chi.go` — build the handler in `BootChi` and
   add one `MountFeature` line in `buildChiMux`.
3. Use **goqite** for async work, the **SSE Hub** for user-facing feedback, and
   **Datastar** for the reactive frontend.
4. Add a `SCOPE:` annotation — `make check-scope` enforces it.
5. Add a `Routes()` method on your handler (absolute `chi` patterns) and pass it
   to that `MountFeature` call — never re-declare the URLs at the root.

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
| Account security | `/account` | Auth-gated. Email verification, password change, TOTP enrollment/recovery, session list/revoke |
| Chat demo | `/chat` | Auth-gated. The one AI demo: conversation + catalog + diagrams + persisted history; keyless demo answers, live model with `GOAI_API_KEY` |
| Review loop | `/review` | Auth-gated. Writer → critic → classifier judge, streamed verdict, discarded. Runs on the `genkit` plugin |
| Whiteboard | `/whiteboard` | Auth-gated |
| Room demo | `/room` | Auth-gated. Heartbeat roster + presenter lock owned by a GoAkt grain |
| Config view | `/config` | Admin-only runtime view; user role/session actions |
| DagNats console | `:8090` / `/dagnats/` | Workflow runs and steps; admin + `ADMIN_EMAILS`, closed when the list is empty |
| Runtime diagnostics | `/debug/statsviz` + `/debug/pprof` | Opt-in (`PERF_ENABLED`, dev on / prod off); operator-only (admin + `ADMIN_EMAILS`), closed when the list is empty. statsviz runtime metrics + stdlib pprof |

Details in [Admin & Dashboard](admin-dashboard.md).

## Chat: one demo, one vocabulary

Chat (`/chat`) is the AI demo: a persisted conversation where the model
answers with invented visuals, catalog components, diagrams, and the app's
own data. A bad answer degrades to the prose around it (a ```mermaid fence
anywhere still renders as a diagram); follow-ups reuse the thread as
context. Operator toggles: `GENUI_CHAT_SURFACES` selects the bands,
`JEV_ROUTER_ENABLED=true` lets a jev-compatible classifier pick which
component an answer leads with (a hint, never a narrowing).

**The three bands, split by who owns the content:**

- **`catalog`** — the fixed generative registry. The model picks a
  component name and fills typed props. Static: nothing depends on app
  state.
- **`custom`** — `html_block`. The model composes markup for this
  request; sanitized server-side through a closed element allowlist and a
  safelisted class vocabulary. This is the only band whose content the
  model invents wholesale.
- **`native`** — the app's own panels (`todo_list {filter}`). The APP owns
  the content (real rows from the store); the model picks only the slice.
  Greetings and other smalltalk never use it: a "hi" is answered briefly,
  never with the user's rows. A panel's `Render` MUST return the feature's own
  page components (row, card, dialog), parameterized only by the write target
  and slice; a chat-specific copy drifts (the first todo panel lost the shared
  badge, author, age, delete gate and confirmation dialog). Contract and
  checklist: [native panels](native-panels.md).

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

Tune the bands with `GENUI_CHAT_SURFACES`
(CSV; absent = the default above; an unknown name fails the boot) or per
request from the composer's `catalog` / `custom` / `native` toggles (plain
DaisyUI toggles; all three off is prose-only and the chat still works).
Turning `custom` off makes Chat catalog-only (the strict showcase: unknown
types fail closed, one bounded repair attempt feeds the failure back).
`none` is prose-only. Answers that reach for a band the tab turned off are
repaired once, then replaced by an honest line instead of a silent hole.

**The reader lifts shape slips, never content.** Two answers a live model
produced for "what are my tasks?" — the component name as the KEY
(`{"todo_list":{...}}`) and props inlined beside the type — are recovered
before validation. The lifted name must be registered and every lifted prop
still goes through the strict decoder, so a name nobody installed and a field
the component does not declare both fail closed: the shape is recovered, the
content is still checked.

A directive with NO name is the one gap the catch-all cannot describe (there
is nothing to put in the bubble), so it counts as a failure: the answer takes
the one repair attempt, then the readable fallback — never an internal "the
model asked for an unnamed component" line. A single `text_note` answer is
stored as prose with no component, so one bubble does not print the same
sentence twice.

**Partial render, catch-all, metrics.** A partially valid envelope renders
what validates: unknown types become a named `text_note` placeholder (the
catch-all), undecodable props are dropped and counted, and the model's
surrounding prose is kept as the lead-in — a half-good answer beats a prose
fallback that hides the parts the model got right. Shapes a repair can fix
(dropped directives, payload with no prose) go back to the model once with
the failure fed back; prose never does. Every answer logs one outcome line
(`chat: answer ready` with `components`, `dropped`, `fallback`, `diagrams`)
— the repair rate per component is the number that says whether the prompt
or the catalog is the thing to fix.

## Removing a feature

Every feature package carries a `SCOPE:feature` annotation. The rule:
delete the package directory, delete its dependents, remove the wiring call
from `internal/server/chi.go`, and — if it was a plugin — remove the
`start*` call in `cmd/web/main.go`. Full checklist in
[Scope taxonomy](scope-taxonomy.md#removing-a-component).

## Client weight

Vendored JS per feature, generated rather than typed: `make check-docs`
measures every file a row names and refuses a number that disagrees, and
`make docs-index` rewrites it. A row that lists several files sums them
before rounding up, so the number is what the page fetches, not what one
file weighs. Gzip is Go's `compress/gzip` (not byte-identical to GNU `gzip`,
which is why one implementation owns the number). The landing quotes this
table through `data-weight` spans and never re-types it. CSS is one shared
bundle and is not attributed per feature.

| Feature | Files | Raw | Gzip |
|---|---|---|---|
| Room, Landing, Config | inline only (no extra JS) | 0 KB | 0 KB |
| Chat diagrams | `pele.global.js` + `diagram.js` (progressive enhancement: source stays readable without them) | 716 KB | 278 KB |
| Todo date filter | `cally.js` (the `<calendar-date>` web component DaisyUI styles as `class="cally"`) | 38 KB | 13 KB |
| Shared notes | `notes.js` | 44 KB | 16 KB |
| Whiteboard | `whiteboard.js` + `rough.min.js` | 74 KB | 26 KB |
| Sounds (plugin, the rail mute) | `cuelume.js` + the modules it imports under `web/resources/static/cuelume/` | 27 KB | 10 KB |
| Pages with a live stream (Todo, Room, Chat) | `sse-open.js` | 8 KB | 4 KB |
| Offline policy + outbox (every shell page) | `offline-policy.js` + `offline-outbox.js` | 30 KB | 11 KB |
| Todo offline renderer (only with offline sync) | `todo-offline.js` | 16 KB | 6 KB |
| Service worker (navigations only, registered on first authenticated load) | `sw.js` | 5 KB | 3 KB |
| Shared shell (every page) | `datastar.js` + `theme.js` + `session.js` + `command-palette.js` | 51 KB | 21 KB |

Whiteboard and notes pages additionally load the shared
`throttle.js` (~2 KB raw, ~1 KB gzip) — leading + trailing throttle
for cursor/caret reports, one implementation for both features — and
`offline-policy.js` (~3 KB raw, ~1 KB gzip): one definition of which HTTP
answers are transient (retry) and which are a verdict (drop), shared by the
three page-side outboxes, so they cannot disagree.

Split rule (Datastar vs vanilla, enforced by review): list fragments
and UI state that patch cleanly go through the Datastar runtime (todo,
whiteboard list, notes index — every one of those pages loads
`datastar.js`, pinned by test); canvas rendering, textarea math, and
op batching stay vanilla JS (no declarative equivalent, and the offline
outbox needs exact control). Shared chrome counts as UI state: the
shell's user menu carries `data-on` handlers, so every shell page loads
the Datastar runtime too (board, notes doc, room, config, landing, login)
— a `data-on`/`data-bind` attribute on a page without the
runtime is dead markup, and here it would silently skip the logout
service-worker cleanup.

Session visibility uses the stream a page ALREADY has: notes/whiteboard send
a `collab.SessionEvent` frame, and todo/room/chat carry a
`sessionAuthed` signal on their feature stream (revalidated on its heartbeat).
Only the stream may write it: a page-level response cannot know the session,
so a zero-valued field on a shared signals struct serializes as
`sessionAuthed:false` and raises the banner on a perfectly signed-in tab (todo
did exactly that on every list/search response, which is why `todo.Signals`
has no such field and the stream merges the signal itself).
The `/api/session/stream` leader stream is opened only by a page with no
feature stream (one VISIBLE tab per browser via Web Locks, BroadcastChannel for
the others). One long-lived connection per page is the rule: browsers cap
connections per host, and a second stream per page has been measured to starve
a later same-tab navigation (the smoke's seventh page hung on `load`, and the
notes caret suite fills the budget with five same-user tabs). The leader stream
also carries the rail's unread count on those pages; a page that already holds
a feature stream refetches the badge on visibility/focus/online instead of opening a second
connection. No tab schedules a request, so every tab
raises the same `components.SessionBanner` the moment a
cookie expires and updates the same inbox badge when the count changes. The
banner never auto-redirects (unsent work would be stranded). It is opt-in per
page via the render-time `authed` flag, so a public page never nags about a
session it never had.

Method: `for f in <files>; do wc -c < $f; gzip -nc $f | wc -c; done`.
`datastar.js` is 34 KB raw / ~13 KB gzip — that is the "~12 KiB client"
in the README (wire size). Icons are inline SVG vendored into the page HTML
(`internal/icon`, zero separate requests); sounds (`cuelume.js`, 10 KB) only
with the sounds plugin.

## Related

- [Seven async layers](async-layers.md) — the topology behind these capabilities.
- [The Todo example](todo-example.md) — the pattern to imitate.
- [Desktop & Mobile](desktop-mobile.md) — two more frontends over the same backend.