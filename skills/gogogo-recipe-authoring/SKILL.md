---
name: gogogo-recipe-authoring
description: "Authoring and owning a gogogo recipe — the copy-and-evolve extension model. Triggers when: adding a feature to a gogogo project, installing or editing a recipe, filling a slot (dashboard card, settings section, row actions), declaring routes/tables/jobs in a receipt, updating a recipe after editing it, or submitting one to the catalog. Covers what a recipe may own, what it must never touch, and how to stay upgradeable once you have changed the code."
---

# gogogo recipe authoring

A gogogo recipe is **not a package you depend on**. It is a working slice — pages,
handlers, components, tables, a queue job, policy rows — copied into your repository,
with a receipt recording where it came from, and **intended to be edited**. You own the
code the moment it is written.

That single fact decides everything below. Read this before creating a recipe, before
editing a recipe's copied code, and before declaring anything in a receipt.

## The three parts of a recipe, and who owns each

| Part | Lives in | Owner | Edit it? |
|---|---|---|---|
| **Code** — handlers, components, domain | your tree (`features/<id>/`, or wherever you put it) | **you** | yes, freely — this is the point |
| **Receipt** — `.recipes/<id>.yml` | your tree, committed | you, but it mirrors facts | only through tooling |
| **Declaration** — routes, tables, jobs, slots, policy | inside the receipt | the engine applies it | never hand-edit |
| **DDL** — `migrations/**.sql` in your package | your tree, committed | you | yes, like any code you own |

The rule that follows, and it is the one thing to get right:

> **Code and schema are both copied; ownership is declared.** A recipe ships its own DDL
> under `migrations/` and the ledger applies it under the recipe id, so two recipes may
> both number from `0001`. The declaration must match that SQL in both directions: a
> declared table with no `CREATE` fails the gate, and a `CREATE` the declaration omits
> fails it too. Never own a core table or a column on one — a side table is the answer.

## Before you write: does it already exist?

Check in this order, and stop at the first hit:

1. **The official catalog** — install it instead of writing it. One command, and you get
   a receipt you can update.
2. **The reference slice** — `features/todo` is the living example every recipe is derived
   from: handler shape, page shape, signals, policy rows, the receipt. Copy it rather than
   inventing a structure.
3. **The scaffold** — `go run ./cmd/gogogo new feature <name>` writes a handler, a page,
   the wiring, the capability entry, the policy rows, then proves the build. Use it as the
   starting point; a hand-rolled slice will miss a gate.

Installing a recipe is a *build* operation, not a runtime one: nothing is resolved at
run time, so nothing drifts on a running host.

## What a recipe may own — and the four things it may never do

**May own:** its directories; its own tables (prefixed with its id); its own routes
(absolute patterns); its own job types (prefixed `ext:<id>:`); its content in a declared
slot; its own policy rows.

**May never:**

1. **Own a column on a core table.** Need extra data about a core entity? Own a side table
   keyed by the entity's id. `todos` (the legacy seed), `users`, `sessions`, `security_events`,
   `schema_migrations` and `config` are core, and a declaration naming one is refused.
   The built-in `todo` unit is the one deliberate exception: it ships its
   own `ext_todo_todos` table through the same declaration gate, so runtime
   queries may only target that table and a recipe can never claim `todos`.
   The check reads your SQL too, not only the declaration: a migration that `ALTER`s,
   `DROP`s, indexes, seeds or triggers a core table is refused at boot, while a foreign key
   that merely *references* one (`REFERENCES users(id)`) is the correct way to scope your rows.
2. **Add a step inside a core handler.** No hook that runs "when the user saves a todo".
   If you need that, the hook belongs in the extension surface as a real seam — or the
   feature does not belong in a recipe yet.
3. **Render a page's frame.** Fill a slot with content; the owning page owns the card,
   the grid and the layout. A recipe that draws its own frame breaks when the page changes
   its layout, which is exactly what the slot contract exists to prevent.
4. **Ship a migration the user is expected to edit.** See above.

## Slots: the four declared shapes, three of them rendered

A slot has a **declared shape**, because a slot without a container contract means
"somewhere in this page" and that is where layout rots. A shape is not an offer:
only a slot with a **host page in this build** can be filled.

| Slot | Shape | Rendered in | You supply |
|---|---|---|---|
| `settings.section` | `section` — a block in the settings column | the settings page | a form section plus its **own** save endpoint |
| `nav.sidebar` | `entries` — a list of destinations | the shell rail | a label and a href, not a fragment |
| `record.row-actions` | `actions` — declarative data | the todo row | label, href, verb, optional confirmation — **not** a component |
| `dashboard.card` | `cell` — one grid cell, narrow | the dashboard page (`features/dashboard`, GET `/dashboard`) | a title and rendered content; the dashboard owns the card and the grid |

`record.row-actions` is data on purpose. A component would need the owning recipe's entity
and its authorization decisions, which is the hook-inside-a-core-handler failure arriving
through the UI. Declared as data, the owning page keeps its authority.

A slot with no host page is refused like an unknown id — at registration, by `recipe install`
before a single file is copied, and by the catalog review. It is never accepted and then
dropped, because a contribution nothing draws is indistinguishable from a feature nobody built.

## Declaring, so the drift check can work

The receipt is what makes "is this still what it claims" answerable. It records:

- `upstream{repo, ref, path}` — where the pristine baseline lives
- `files{path → sha256 of the pristine content}` — **hashes, not a version string**: a
  version cannot tell you which lines you changed, and its absence is why shadcn's diff is
  only two-way
- `declaration{tables, routes, jobs, slots}` — the append-only surface

Keep the declaration in step with the code. A declared route the code does not register is
a drift **failure**, not a warning: it ships a nav entry that leads nowhere. A declared job
the code does not register burns CPU forever.

## After you edit the code — this is the part people get wrong

Editing is expected. Losing the ability to update is the cost, and it is manageable:

| What you did | What an update does |
|---|---|
| Changed nothing | fast-forwards silently |
| Edited a file upstream did not touch | **your bytes are kept**, never overwritten |
| Edited a file upstream also changed | a real conflict, resolved by git — not guessed |
| Deleted a file | stays deleted; not resurrected |
| Upstream dropped a file you never touched | cleaned up |

So: **keep your changes in commits, not in one giant uncommitted blob.** The update path
uses git as the arbiter, and the receipt supplies the third input git does not have — the
pristine baseline. A user who edits and never commits has thrown away the only thing that
makes an honest merge possible.

If you diverge substantially, say so in the recipe's own README: what you changed and why.
That note is what a future you (or a successor) reads before deciding whether to take an
upstream fix.

## Submitting to the catalog — optional, always

An extension is usable the moment it builds. A catalog entry is **a review, not a licence
to exist**. Submit when you want the claim "this was read, its declaration matches its
code, and it behaves under these installed sets".

A reviewed entry records the version, the file hashes, the dependency closure, the slots,
and the installed sets the review actually exercised. Two consequences worth knowing before
you submit:

- **The review is per version.** A new version needs a new review; do not expect an entry
  to carry forward.
- **The dependency closure is part of the cost.** A recipe that drags a model framework
  costs every operator tens of megabytes of binary; the same recipe using only the
  standard library costs almost nothing. Declare it, because it is invisible in your diff.

## Actions: which routes need a headless verb

A user who builds on gogogo does not open your UI to work — an agent does,
headless, through `gogogo extension invoke` / MCP `extension_invoke`.
Every mutating route (POST/PUT/PATCH/DELETE) and read route (GET) your
recipe declares must either:

- appear as an `InvokeAction` (closed verb, declared route, closed
  `Params` set) so the call can be discovered and executed headless, or
- be explicitly omitted: auth endpoints (login, signup, logout),
  streaming routes (`/stream`, `/events` — they never complete headless),
  parameterized routes (`{id}` — refused until the headless id is
  typed), and destructive-only endpoints you do not want agents to run.

The gate enforces both directions: an action whose route is not declared
is uncallable, and a receipt whose verbs drift from the code is a
failure. A mutating route with no verb is *invisible* to headless
users — that is the gap, not a style choice.

### When the deferred pieces become necessary

| Piece | Trigger to build it | Signal that does **not** trigger it |
|---|---|---|
| **Parameterized routes (`{id}`)** | A concrete verb needs a real id headless (e.g. `todo toggle 42`) and you can name the encoding. | Guessing id from path, or a `.templ` row action already covering the UI case. |
| **Typed param values** | A handler genuinely needs non-string input (int/bool/list) that form parsing mishandles. | Wanting JSON-schema aesthetics; names-only is enough until then. |
| **Machine identity (API token)** | An external client must act headless without a browser login. Reuse the `Role` subject verbatim. | "I'll need auth later" with no external consumer yet. One auth pillar only. |
| **Remote client (non-loopback)** | An operator must drive a deployed server from another host. | Adding a public URL before the machine-identity policy exists. Loopback is the v1 trust boundary. |

## The install contract

An installable recipe directory carries a manifest beside the tree it copies:

```yaml
# .recipe.yml — the installable half. The receipt (.recipes/<id>.yml) is
# written by the installer; this file is what you author.
id: pomodoro
version: v1.0.0
upstream:
  repo: github.com/calionauta/gogogo
  ref: v0.56.0
module: github.com/calionauta/gogogo
register: features/pomodoro/handlers
declaration:
  routes:
    - {method: GET, pattern: /pomodoro}
  tables: [ext_pomodoro_session]
  actions:
    - {verb: start, route: {method: POST, pattern: /pomodoro/start}, params: [secs]}
```

`version` is the author's version claim (`vMAJOR.MINOR.PATCH`). Install
records it in the receipt, and `recipe outdated` compares it with the latest
reviewed catalog entry (`catalog/<id>/<version>.yaml`) — omitting it means
the freshness check honestly reports unknown.

`register` names the copied package that exports the one function the composition
root knows:

```go
func Register(reg *recipe.Registry, d register.Deps) {
	reg.Add(NewHandler(d.Cfg, d.Auth, d.Queries).Recipe())
}
```

`module` is the module your imports were written against; the installer rewrites
it to the target tree's own module in copied `.go`/`.templ` files (never in
prose). `register.Deps` is the seam's promise (config, auth, store, SSE hub). A
field appears there by a core decision, not by request: needing something else is
a conversation about the contract, never a local workaround.

The installer turns receipts into `internal/server/recipes.gen.go` — the one
generated file `buildChiMux` calls. Never edit it by hand: `gogogo recipe
install|update|uninstall` regenerates it, and a removed recipe is skipped so the
file never imports a package that is gone. The receipt keeps the record after an
uninstall, because the data still needs an owner.

**Install finishes the job the tree needs to build.** It does not hand you a list
of follow-up commands:

- **Pages.** The copied `.templ` files are generated with the module's own pinned
generator (`go tool templ generate`), so the tree compiles without you running
`make templ`.
- **Queries.** A recipe that declares tables gets its DDL mirrored flat into
`internal/db/recipe_schema/<id>__<migration>.sql`, which is where `sqlc` reads it,
and install then runs `sqlc generate`. `sqlc` is a standalone binary, so when it is
not on `PATH` the install says so and names `make sqlc` — that is the one step an
install can defer, never a step it leaves you to discover.
- **Policy.** The declared routes get their rows in `internal/rbac/casbin/policy.csv`,
inside a block marked with the recipe id. Without it every signed-in user gets a 403
in production: the policy decides reachability by path, and a route it has never seen
is denied. `recipe uninstall` removes exactly that block, and a re-install rewrites it
from the declaration — so never hand-edit inside the markers, and never add a row
beside them for a route the recipe declares.

Ship the migrations at `<your package>/migrations/**.sql` — the path segment is the
only anchor the installer has, since it cannot see inside your `Migrations()` call —
and leave the mirror alone: `recipe status` reports a migration edited after install,
and `recipe install` is what refreshes the copy.

A recipe that contributes a sidebar entry **moves the shell's page digests**, so
the page-structure gates go red on install and again on uninstall. That is the gate
working, and the installer prints the command that re-blesses them
(`PAGES_DIGEST=update go test ./internal/server -run TestPagesDigest`); read the
diff before you run it.

## Checklist before calling it done

- [ ] The declaration names every route, table, job and slot the code actually provides
- [ ] Every table is prefixed with the recipe id; no core table or core column is claimed by a
      declaration OR acted on by a statement (`ALTER`/`DROP`/index/seed/trigger)
- [ ] Every declared slot is one this build renders, and your code contributes it (`recipe status`
      reports a declaration that names a slot nothing contributes)
- [ ] Every job type is prefixed `ext:<id>:`
- [ ] No route collides with an installed recipe (the seam refuses this before the build,
      naming both owners — read the message, it tells you both)
- [ ] Slot content renders inside the owner's frame, with the declared shape
- [ ] `features/todo` was consulted before inventing a structure
- [ ] `version: vX.Y.Z` declared in `.recipe.yml` (`recipe outdated` compares it against the reviewed catalog entry — without it the freshness check honestly reports unknown)
- [ ] Your deliberate edits are committed, and the recipe README says why you diverged
- [ ] The build is green, including the gates the project already enforces

## When the recipe model does not fit

Say so rather than bending it. Three cases where it genuinely does not:

- **You need a hook inside a core handler.** That is a request for a new seam, not a recipe.
- **You need to add a column to a core table.** Own a side table instead.
- **You need install without a rebuild.** Then you need an out-of-process seam, which this
  project deliberately does not ship as the default: a local socket round trip measures
  ~22 µs, which is unusable in a per-row render path. Ask before building it.
