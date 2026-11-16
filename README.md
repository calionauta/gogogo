# gogogo

<p align="center">
  <img src="web/resources/static/logo.png" alt="gogogo" width="420">
</p>

<p align="center">
  <b>The foundation is done. Your agent ships the product.</b>
</p>

<p align="center">
  <a href="https://calionauta.github.io/gogogo/">Landing page</a> ·
  <a href="https://calionauta.github.io/gogogo/docs/">Docs</a> ·
  <a href="https://gogogo.calionauta.com/">Live demo</a> ·
  <a href="https://github.com/calionauta/gogogo/releases">Releases</a> ·
  <a href="https://github.com/calionauta/gogogo/actions">CI</a>
</p>

---

A complete Go product — backend, frontend, database, auth, jobs, realtime,
durable workflows and offline writes — that compiles to **one binary**. Every
slice is code you own and can delete; every heavy dependency has an off switch.
A CLI and an MCP server let an agent change the tree and prove the build
instead of guessing at it.

**Not a framework.** Nothing is versioned out from under you: the scaffold
copies the tree, and `git` is the update path.

## Quick start

```bash
curl -sSfL https://raw.githubusercontent.com/calionauta/gogogo/master/install.sh | sh -s -- --run
# Open http://localhost:8080  ·  demo1@demo.app / demo1234456
```

Manual: `gh repo create my-app --template calionauta/gogogo --clone`, then
`make rename NAME=my-app`, `make dev`.

## Try it live

| | |
|---|---|
| **Todo & Whiteboard** | <https://gogogo.calionauta.com/> |
| **DagNats console** | <https://gogogo.calionauta.com/dagnats/> |

## What ships

One row per thing you get. The **opt out** column is the runtime switch — flip
it and the binary still builds.

| Capability | Technology | Opt out |
|---|---|---|
| **Web UI** | Templ + Datastar (SSE), DaisyUI palettes | — |
| **Persistence** | Turso/SQLite via sqlc | — |
| **Auth** | Owned argon2id sessions, magic link, TOTP | — |
| **Access control** | Casbin v3 policy over `users.role` | `RBAC_ENABLED=false` (prod) |
| **Background jobs** | goqite + retry, queue-backed | — |
| **Durable workflows** | DagNats over NATS JetStream | `DAGNATS_ENABLED=false` |
| **Realtime fan-out** | SSE Hub + NATS JetStream | `NATS_ENABLED=false` |
| **Offline writes** | Write-ahead outbox, app-shell cache, leaf node | `OFFLINE_SYNC_ENABLED=false` |
| **Collaboration** | Loro CRDT (notes, whiteboard) | delete by hand |
| **Entity actors** | GoAkt grains (rooms, presence) | `GOAKT_ENABLED=false` |
| **AI** | GoAI (any provider) + Genkit flows | `GOAI_API_KEY` unset |
| **Metering** | Credits + BYOK | `CREDITS_ENABLED=false` |
| **Search** | Own layers: substring, fuzzy, BM25 | delete by hand |
| **Admin** | `/config`: audit trail, users, runtime switches | — |

Details per row, with the removal class of each unit and its exact removal
path: [docs/features](docs/features.md).

## Stack

```text
language   Go 1.27
frontend   Templ · Datastar (SSE) · Tailwind v4 + DaisyUI v5
data       Turso/SQLite · sqlc · DB-backed sessions
security   argon2id · Casbin RBAC · security_events audit trail
async      goqite · DagNats · NATS JetStream · GoAkt · Loro CRDT
ai         GoAI (any provider) · Genkit flows · jev-like classifier seam
shipping   one binary · make build · env-var opt-outs
```

One `make build` compiles all of it: no build tags, no second boot path, no
matrix, no runtime CSS or JS build, no service to operate.

## App shapes: one template, five products

The same `cmd/web` binary ships as a full product, a web app, an API-only
backend, or the server behind the desktop and mobile shells. `APP_SHAPE` (or the
committed `.gogogo/shape.yml` record) picks the entrypoints and the surfaces.

```bash
APP_SHAPE=api make build   # one binary; page routes answer 404
```

`api` prunes page routes at mount, so the 404 comes from the route tree, not
from a handler that runs and refuses. `/api`, `/health` and the static assets
keep working. Enum, record and the `check-shape` gate:
[docs/shape.md](docs/shape.md).

## Recipes: read one, then own one

A recipe is a working slice — pages, handlers, tables, jobs, policy rows — not a
package you depend on. Two modes, and the difference is a receipt:

| Read (default) | Own (explicit) |
|---|---|
| `recipe source <id>` prints the code | `recipe install <id>` copies it in |
| No receipt, no copy, no update debt | Receipt at `.recipes/<id>.yml` |
| Adapt the pattern into your own slice | Your edits survive: pristine hashes + `git merge` |

| | |
|---|---|
| **Checked, not trusted** | `recipe check` scores it; `recipe test` runs its tests |
| **What it declares** | routes, tables, jobs, slots, metrics, panel |
| **Reviewed versions** | [`catalog/`](catalog) — bytes, not names |
| **What to read first** | [`catalog/reference.yml`](catalog/reference.yml) |
| **What is installed here** | `go run ./cmd/gogogo recipe list` |

A fresh scaffold drops the demo slices (`--recipes none` is the default):
`chat`, `kiosk`, `notes`, `review`, `room`, `whiteboard`. `--recipes all` keeps
them, `--recipes <ids>` drops exactly those. `todo` is removable on the same
terms but stays today: `internal/desktopboot` (the native shells' shared boot),
`cmd/gui` and two test files import it, and the plan prints that list rather than
removing a package something still uses. Read one with `recipe source`; copy one
with `recipe install` when you want it owned.

Languages are one tracked record, not a flag per command:

```bash
go run ./cmd/gogogo locales            # what this product ships, and why
go run ./cmd/gogogo locales --set en,pt-BR
```

`new feature` seeds one catalog per recorded tag, `recipe check` grades coverage
against it, and `make check-locales` fails a branch where a recipe translates
one shipped language but not another. A metric label is translated in the
declaration's own `translations:` block (it is data, so it cannot live in the
app catalog), and `en` is the fallback spelling — never a required entry.

Contract: [skills/gogogo-recipe-authoring](skills/gogogo-recipe-authoring/SKILL.md) ·
Taxonomy: [docs/scope-taxonomy](docs/scope-taxonomy.md).

## For AI agents

- [`llms.txt`](llms.txt) — what to read per task.
- `go run ./cmd/gogogo advise|doctor|generate|shape|recipe|export|new feature` —
  the same evolution engine the MCP server (`cmd/gogogo-mcp`) exposes.
- Those tools build and evolve a checkout. A product's users and their agents
  call the running server (`POST /api/ext/invoke`) under session and RBAC, never
  the installer MCP. See
  [docs/mcp.md](docs/mcp.md#installer-surface-vs-product-surface).

## Commands

```bash
make dev       # live reload
make build     # one binary
make ci-local  # the full gate (= CI)
make site      # rebuild the docs site
```

More: [getting-started](docs/getting-started.md#commands).

## License

MIT — see [LICENSE](LICENSE). Feedback and PRs welcome.
