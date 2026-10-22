# gogogo

<p align="center">
  <img src="web/resources/static/logo.png" alt="gogogo" width="420">
</p>

<p align="center">
  <a href="https://calionauta.github.io/gogogo/">Landing page</a> ·
  <a href="https://calionauta.github.io/gogogo/docs/">Docs</a> ·
  <a href="https://github.com/calionauta/gogogo/releases">Releases</a> ·
  <a href="https://github.com/calionauta/gogogo/actions">CI</a>
</p>

> **Your stack is ready. Come build.** Every decision favors practical outcomes
> over abstract ideals. The stack optimizes for simplicity, consistency, and
> shipping software with minimal friction.

Every web project starts with the same conversation — pick a database, auth,
router, reactive frontend, task queue… — and the project stalls at the
*decisions*, installations, and configurations. Not at the code. This template
answers those decisions once, ships them wired together, and documents how to
change or remove each piece.

What it is not: a framework. Frameworks call your code and keep you on
their release train; gogogo hands you wired, documented code and gets out
of the way.

## 📖 Docs (single source of truth)

The manual lives on the site, not in this file:
**[calionauta.github.io/gogogo/docs/](https://calionauta.github.io/gogogo/docs/)**

<details>
<summary>Page index</summary>

Generated from the docs site manifest (`site/docsite.config.mjs`) — edit the
manifest, then `make docs-index`; `make check-docs` fails when this block drifts.

<!-- docs-index:begin -->
**Get started**
- [overview](docs/overview.md) — What gogogo is, who it is for, and the seven async layers it ships.
- [motivation](docs/motivation.md) — The thesis behind the stack: Go for almost everything, Zig as the named exception, and how to choose without hype.
- [architecture-decision](docs/architecture-decision.md) — The flat-stack decision record: what it serves, what it replaced, and the migration it completed.
- [inspirations](docs/inspirations.md) — What gogogo learned from other projects, credited rather than vendored.
- [getting-started](docs/getting-started.md) — Clone, run, first five minutes, and the commands you need.

**Agents**
- [cli](docs/cli.md) — The installer as scriptable commands: advise, preview, apply, add, check.
- [mcp](docs/mcp.md) — Install the MCP server, wire it into your client, and the tools it serves: opinions, trim, drift, migration.

**Core**
- [architecture](docs/architecture.md) — Directory layout, dependency direction, entry points, and route wiring gotchas.
- [stack-layers](docs/stack-layers.md) — Every dependency and what it buys, layer by layer.
- [stack-tradeoffs](docs/stack-tradeoffs.md) — What each choice rejected, what it costs to keep, and when to revisit: Turso vs SQLite vs Postgres, Datastar vs React, and the rest.
- [features](docs/features.md) — Every capability and its runtime opt-out.
- [search](docs/search.md) — The search plugin: one seam, three layers (substring, fuzzy, BM25), the runtime picker, and what trimming it leaves behind.
- [api-routes](docs/api-routes.md) — Generated inventory of every API route, pinned by tests — the trim and hook gate that proves no route is lost.
- [unit-contracts](docs/unit-contracts.md) — Generated card per capability: routes it registers, generated queries it calls, runtime switch, and what removing it takes.
- [no-auth-no-db-recipe](docs/no-auth-no-db-recipe.md) — Recipe for stripping auth and the database out of a clone that needs neither.
- [use-cases](docs/use-cases.md) — What each core, plugin, and feature is for, in business language.
- [scope-taxonomy](docs/scope-taxonomy.md) — Core / Plugin / Feature: the rule for deciding what is safe to delete.
- [porting-into-an-existing-app](docs/porting-into-an-existing-app.md) — Taking one unit into an app that already exists: export, copy, rewire, prove — and the four things no recipe can know.
- [db-concurrency](docs/db-concurrency.md) — Turso single-writer stance, the probed MVCC path, and revisit triggers.
- [configuration](docs/configuration.md) — Every environment variable and runtime constant.
- [rbac](docs/rbac.md) — Casbin RBAC: role vocabulary, the feature checklist, hint-vs-enforcement, and the two invariants that bite (authentication is not authorization; policy objects are paths, not regex).
- [dependency-principles](docs/dependency-principles.md) — Write it or take the library: six principles, the ordered decision procedure, the closed human gate, and the trade-off line a reviewer reads.
- [exception-to-go](docs/exception-to-go.md) — Go-first policy: why Zig is an exceptional native boundary, the agent decision procedure, and the isolation, testing, and removal rules.
- [native-language-tradeoffs](docs/native-language-tradeoffs.md) — Evidence-backed tradeoffs of Zig, Rust, Nim, Odin, and Mojo as native exception languages, and what a non-Zig proposal must rebut.

**Async**
- [async-layers](docs/async-layers.md) — The seven async layers, SSE Hub record-sync vs ephemeral signals, cross-instance and offline sync.
- [realtime-recipe](docs/realtime-recipe.md) — How to add live UI: client/server patterns, checklist, no invented transports.
- [dagnats](docs/dagnats.md) — DagNats capability table, version bounds, learned gotchas, and the add-a-workflow path for LLM agents.

**AI**
- [llm-and-credits](docs/llm-and-credits.md) — GoAI configuration and the optional ai-credits / BYOK plugin.
- [review](docs/review.md) — The review loop: Genkit flows, goai generation, and a Jev-compatible classifier judge.

**Frontend**
- [todo-example](docs/todo-example.md) — The Todo reference implementation and the contract to imitate.
- [ui-skins](docs/ui-skins.md) — The one shell, the skin seam, and the DaisyUI themes.
- [ui-sounds](docs/ui-sounds.md) — Vendored cuelume sound feedback and its accessibility contract.

**Research**
- [research](docs/research/index.md) — One place for every research note, comparison and tradeoff decision in this repo: what was evaluated, when, and what was chosen.
- [auth-libraries](docs/research/auth-libraries-2026-10.md) — The 2026-10-21 evaluation of limen against owning argon2id and sessions: schema ownership, version skew, and what flips the decision.
- [ui-engine-research](docs/research/ui-engine-research.md) — The 2026-10-21 engine and component-library evaluation behind the UI stack, with local benchmarks and the Rocket assessment.
- [ui-components](docs/research/ui-components-2026-10-21.md) — The 2026-10-21 comparison of DaisyUI, Starbase, datastar-ui and dsx on Datastar, SSE, lightness, performance, functionality and forkability.

**Ship**
- [deploy](docs/deploy.md) — Server layout, first-time setup, deploy workflow, build pipeline and version badge.
- [desktop-mobile](docs/desktop-mobile.md) — Wails v3 desktop/mobile, edge sync, and the native window.
- [admin-dashboard](docs/admin-dashboard.md) — Admin surfaces, the single-cookie session, and the DagNats console.

**Operate**
- [upgrading](docs/upgrading.md) — Keeping a scaffolded project current: the recorded base revision, --check-update's two halves, and migrate as a list plus gates — never a merge.
- [local-ci](docs/local-ci.md) — The five-tier feedback loop and make signoff.
- [ci-triage](docs/ci-triage.md) — When CI fails on something variable: the 60-second triage, flake diagnosis, quarantine with an expiry, and why 100% is not the target.
- [code-quality](docs/code-quality.md) — The 33 linters, how to scope them, the Datastar-specific rules, and the ruleguard rules that encode project footguns as CI failures.
- [troubleshooting](docs/troubleshooting.md) — Symptoms and where they actually come from.
- [duckdb](docs/duckdb.md) — Query JSONL logs and the Turso file with DuckDB: local operator recipe.
<!-- docs-index:end -->

Plus [ARCHITECTURE.md](ARCHITECTURE.md) — the canonical annotated dependency
graph, and [AGENTS.md](AGENTS.md) — the working rules for human and AI agents
in this repo. The rules an agent is meant to *load* live in
[`skills/gogogo-coding-standards`](https://github.com/calionauta/gogogo/tree/master/skills/gogogo-coding-standards)
(Go + template standards, concurrency, perf, testing, the exception-to-go gate) — install
it with `npx skills add calionauta/gogogo`.

</details>

---

## 🚀 Quick start

This repo is a **GitHub template** — click **Use this template** to get your own
copy, then rename it. Do not build inside a clone of the template itself: your
module path would stay `github.com/calionauta/gogogo` and
deploys would target the template's server directory.

```bash
# 1. Scaffold with the installer: one command installs (binaries +
#    Go toolchain when missing), asks name/owner/plugins/features, renames,
#    trims what you skip, writes AGENTS.md, then `make dev` takes this
#    terminal — Ctrl-C stops dev. Needs curl + git, nothing else.
curl -sSfL https://raw.githubusercontent.com/calionauta/gogogo/master/install.sh | sh -s -- --run

# With Go already installed, the equivalent without curl|sh:
# go run github.com/calionauta/gogogo/cmd/gogogo@latest --run

# Manual fallback (no installer):
# gh repo create my-app --template calionauta/gogogo --clone
# cd my-app
# make rename NAME=my-app          # add OWNER=myorg if not under calionauta
# make site                        # regenerate the published docs
# make dev                         # run
```

This command needs Go and git (checked up front) — or curl + git via the
binary install in [CLI & MCP](#cli--mcp-agent-paths) below, where
`install.sh` bootstraps Go when missing. Open `http://localhost:8080`
for the landing page, then `http://localhost:8080/todo` for the demo
(sign in with the seeded `demo1@demo.app` / `demo1234456`; `demo2@demo.app` takes the same password for two-user contention).

> `make rename` only rewrites **this** project's identity. Sibling repos under
the same owner (`ai-credits`, `datastar-lint`, `pi-leakguard`) are real
dependencies and are left alone.

> **One `make build` compiles everything.** No build tags, no feature matrix.
> Opt out at runtime with env vars (`NATS_ENABLED=false`, `DAGNATS_ENABLED=false`).

## In the box

| Layer | Choice | Why |
|---|---|---|
| **Language** | Go 1.27 | Fast compilation, easy deploy, lean runtime. Go-first: Zig only as an exceptional native escape hatch ([exception-to-go](docs/exception-to-go.md); the why: [motivation](docs/motivation.md)) |
| **Zero-config database and auth** | Turso Database (SQLite-compatible, sqlc) + owned argon2id sessions | Zero-config Turso, cookie sessions, file-local storage — no separate service |
| **Access control without a service** | [Casbin](https://casbin.org) + `users.role` | Roles on the user row, policy in one model, enforced before the handler; embedded policy by default, DB-backed when evolving ([rbac](docs/rbac.md)) |
| **Reactive UI with no JS framework** | [Templ](https://templ.guide) + [Datastar](https://data-star.dev) (SSE) + Tailwind v4 | Server-rendered HTML, ~12 KiB client, no JS framework build step |
| **One shell, switchable themes** | [DaisyUI v5](https://daisyui.com) + four palettes | One markup for every page; the theme picker swaps palette and radius, never layout |
| **Background jobs with retry** | [goqite](https://github.com/maragudk/goqite) + SSE Hub | goqite executes with backoff; SSE Hub streams progress to the browser. No Redis |
| **Workflows that survive a restart** | [DagNats](https://github.com/danmestas/dagnats) | Multi-step workflows as declarative JSON; resume after a mid-run kill |
| **Room ownership without races** | [GoAkt](https://github.com/Tochemey/goakt) | One grain per room: heartbeat roster plus exactly-one presenter lock, supervised |
| **Shared state that merges offline edits** | [Loro CRDT](https://github.com/aholstenson/loro-go) + [Rough.js](https://roughjs.com) | Conflict-free state with no last-write-wins data loss |
| **Realtime for one instance or many** | SSE Hub + NATS JetStream | Per-user record push, plus cross-instance broadcast |
| **AI features on any provider** | [GoAI](https://github.com/zendev-sh/goai) + optional [ai-credits](https://github.com/calionauta/ai-credits) | Any OpenAI-compatible provider; optional billing ledger + BYOK |
| **Secrets encrypted at rest** | [age](https://age-encryption.org) + `~/.secrets/` | Local encryption. No vault, no cloud |
| **Desktop apps on the same backend** | [Wails v3](https://wails.io) + [gogpu/ui](https://github.com/gogpu/ui) | Native shells over the same backend, plus a no-HTTP frontend |
| **Linting tuned for AI-written code** | [golangci-lint](https://golangci-lint.run) (33) + [datastar-lint](https://github.com/calionauta/datastar-lint) | Tuned for LLM-authored code: unchecked errors, context leaks, lost context propagation |

Full table with the reasoning behind each choice:
[stack-layers](docs/stack-layers.md). Client weight per feature
([measured](docs/features.md#client-weight)): 0–48 KB of JS — the backend
is the source of truth, the browser holds intent and cache.

## Everything async your app will need

Seven complementary layers, already inside, each with its own opt-out:

```
Background jobs that update the UI   → goqite + SSE Hub (always on)
Workflows that survive a restart     → DagNats as JSON (DAGNATS_ENABLED=false)
Collaboration without data loss      → Loro CRDT (remove internal/collab/)
Live record updates, scoped per user → SSE Hub record-sync (always on)
Ephemeral UI signals                 → SSE Hub via Datastar (always on)
Broadcast across instances           → JetStream (NATS_ENABLED=false)
Room ownership without races         → GoAkt grains (GOAKT_ENABLED=false)
```

Records flow through the SSE Hub record event — scoped per user by the
owner column, free of charge. The ephemeral channel is reserved for
ephemeral signals (toasts, client counts, workflow progress). Mixing those two up is the
most common mistake in Datastar apps; [async-layers](docs/async-layers.md)
explains why the split matters.

## Capabilities

Every capability is always compiled. Each has a documented opt-out.

| Capability | Opt-out |
|---|---|
| Todo app + live record-sync, stacked toasts, sound feedback | delete `features/todo/` |
| Queue + retry with exponential backoff, SSE `lastRetry` signal | — (core) |
| AI Suggest via GoAI, or keyless simulated LLM for the demo | no `GOAI_API_KEY` and `SIMULATE_LLM=false` |
| Ask AI: model renders catalog UI (cards, tables, sections) via SSE | `trim genui` (bundle with Chat), or no `GOAI_API_KEY` and `SIMULATE_LLM=false` (demo answers keyless) |
| Chat: conversation over the same catalog, persisted per owner | `trim genui` (bundle with Ask AI), or no `GOAI_API_KEY` and `SIMULATE_LLM=false` |
| Review loop: writer → critic → classifier judge, streamed verdict | `trim review`, or no `GOAI_API_KEY` (real-LLM only; page shows a setup hint) |
| Search layers for feature rows (substring in SQL, fuzzy tiers, BM25 over its own postings) | `trim search` (a consumer keeps the SQL substring layer) |
| AI credits + BYOK relay + Stripe top-ups | `CREDITS_ENABLED=false` |
| Collaborative whiteboard (CRDT + presence + offline outbox) | `trim whiteboard` (bundle with notes + collab transport), or `COLLAB_SNAPSHOTS=memory` |
| Room presence demo (GoAkt grains: roster + presenter lock) | delete `features/room/` |
| Shared notes (server-owned Loro Text, live peer text) | delete `features/notes/` |
| Durable `WelcomeOnboarding` workflow | `DAGNATS_ENABLED=false` |
| Hybrid offline sync (Service Worker + Leaf Node) | `OFFLINE_SYNC_ENABLED=false` |
| Single-store persistence (Turso Database via sqlc) | — (core) |
| Multi-instance realtime | `NATS_ENABLED=false` |
| Public landing page + auth-gated config view | delete `features/landing/`, `features/config/` |
| Wails v3 desktop/mobile + native window | delete `cmd/desktop/`, `cmd/gui/` |

Infrastructure opts out with an env var. The installer automates the common
deletions — named presets included: `go run ./cmd/gogogo --preset minimal`
(core + todo + auth), `no-ai`, `no-collab` — see
[cmd/gogogo/README.md](cmd/gogogo/README.md). Product features also opt out
by deleting the package and its one wiring call — see
[features](docs/features.md).

## Core / Plugin / Feature

Every non-test `.go` file under `internal/` and `features/` carries a SCOPE
annotation declaring its removal class. `make check-scope` enforces it in CI and
in your pre-commit hook.

| Annotation | Meaning | You would |
|---|---|---|
| `SCOPE:core` 🔴 | The binary does not work without it | Customize, never remove |
| `SCOPE:plugin` 🟡 | The binary works but loses a capability | Swap, or delete + remove the wiring call |
| `SCOPE:feature` 🟢 | A demo or add-on | Keep as reference, then remove |

The rule for agents: **never delete a `core` file without asking; never
keep a `feature` file in production if the domain doesn't need it.**
Full taxonomy and per-package removal table:
[scope-taxonomy](docs/scope-taxonomy.md).

## For AI agents

Three entry points, one engine (`internal/installer` — CLI and MCP execute
the same code path with the same receipts and proof):

- [`llms.txt`](llms.txt) — the agent entry index: what to read per task,
  invariants to not relearn. Published map of the docs site:
  `site/llms.txt` (index) + `site/llms-full.txt` (complete texts).
- Installer CLI — `go run ./cmd/gogogo advise --need "..."` for opinions
  (reads nothing, changes nothing; `--intent` turns the opinion into the
  ordered commands for a goal), `--preset minimal|no-ai|no-collab` or
  `--plugins/--features` keep lists + `--dry-run` to preview,
  `gogogo add <unit>` to reverse a trim, `gogogo export <unit> --to <scratch>`
  to take one into a tree that is not gogogo (read-only bundle + porting
  checklist), `gogogo new feature <name>` to scaffold one feature with wiring +
  gates + proof. Full reference: [cmd/gogogo/README.md](cmd/gogogo/README.md).
- MCP server — `cmd/gogogo-mcp` (separate module, stdio): the same engine as
  tools — advice and routes, trim, drift check, version and migration, add,
  export, new feature. The served surface, with parameters, is the table in
  [cmd/gogogo-mcp/README.md](cmd/gogogo-mcp/README.md); that module's test
  asserts it against the catalog, which is why this line names no list to
drift.
  Tool table in [cmd/gogogo-mcp/README.md](cmd/gogogo-mcp/README.md).

## Try it live

| What | URL |
|---|---|
| **Todo & Whiteboard demo** | [gogogo.calionauta.com](https://gogogo.calionauta.com/) |
| **Durable workflow console (DagNats)** | [gogogo.calionauta.com/dagnats/](https://gogogo.calionauta.com/dagnats/) |

Sign in as `demo1@demo.app` / `demo1234456` (`demo2@demo.app` takes the same
password — open a second browser or incognito window to be both at once and
prove contention, shared-vs-private todos, and authorship).

## Commands

```bash
make dev           # Live reload with Air (regenerates templ + CSS)
make build         # Build binary (unified: everything included)
make ci-local      # Full local gate (= CI): templ, lint, checks, build, boot smoke, parallel fan-out (race + browser smokes)
make signoff       # ci-local + gh signoff stamp — the recommended pre-push gate
make setup         # Activate lefthook git hooks + verify agent tooling
make site          # Rebuild the docs site from docs/*.md
```

Full command reference: [getting-started](docs/getting-started.md#commands).

## CLI & MCP (agent paths)

Humans get the guided installer above. Agents get the same engine
(`internal/installer`) through two scripted paths — opinions first,
preview second, mutation last. Full guides live in their own section:
[docs/cli.md](docs/cli.md) and [docs/mcp.md](docs/mcp.md).

| Path | Use when | Entry point |
|---|---|---|
| `advise` | deciding what to keep (use-case → keep/drop + off-switches + Go/Zig rule). Changes nothing | `go run ./cmd/gogogo advise --need "..." --format json` |
| trim `--dry-run` | previewing the scaffold as one JSON document | `--no-tui --dry-run --format json` (add `--yes` to apply) |
| `add` / `--check` | restoring a unit into a scaffolded checkout / verifying markers | `add <unit> --from <pristine> --dir <proj> [--dry-run\|--yes]` |
| MCP server | tool calls instead of shell (`advise_stack`, `capabilities_list`, `trim_plan`, `trim_apply`, `check_tree`, `add_unit`) | `cmd/gogogo-mcp` (own module, stdio) + one JSON block in the client config |

Destructive paths refuse without explicit confirmation (`--yes` /
`confirm:true`). Strategy and the foreign-codebase boundary:
[getting-started §0b–0c](docs/getting-started.md#0b-opinions-before-changes-advise).

### Lifecycle: create → evolve → update → customize

The same questions come back after the scaffold, and each one has a command.
The read-only ones are marked; the rest mutate exactly the tree you point them
at.

| Goal | Command | Changes anything? |
|---|---|---|
| Create | `gogogo --dry-run` then `--yes` (interactive, or `--preset`/`--plugins`/`--features`) | Yes — the new directory |
| Evolve: your own slice | `gogogo new feature <name> --dir .` | Yes — inside your tree |
| Evolve: restore a trimmed unit | `gogogo add <unit> --from <pristine> --dir .` | Yes — inside your tree. Refuses a checkout from a different release than your base |
| Update: is there anything? | `gogogo --check-update --dir .` | No. **Exit 3 means yes** |
| Update: what changed? | `gogogo migrate --dir .` | No — a list you apply |
| Update: which files apply to me? | `gogogo migrate --dir . --files` | No — labels each upstream file with its capability and whether you kept its unit |
| Customize | edit freely, then `gogogo --check --dir .` | No — reports whether a marker moved; your edits win |
| Take a unit into a non-gogogo app | `gogogo export <unit> --to <scratch>` + [porting checklist](docs/porting-into-an-existing-app.md) | Only the scratch directory, and the bundle is stamped with the release it came from |

Agents get the same answers as an ordered route: `advise --intent
update|port|add-unit|new-feature|scaffold` prints the exact commands for this
tree (and refuses to name a command the tree would reject). Details:
[upgrading](docs/upgrading.md),
[porting](docs/porting-into-an-existing-app.md),
[scope taxonomy](docs/scope-taxonomy.md).

No Go on this machine? Tagged releases ship static binaries (`gogogo` +
`gogogo-mcp`, checksummed) — and `install.sh` bootstraps a user-space Go
toolchain when none is found (`~/.local/go`, no sudo, existing installs
untouched). It does not symlink `go` into `~/.local/bin`: that directory is on
your PATH, so the link would win over a system Go. It exports
`~/.local/go/bin` for the install and prints the line to persist it. Same one-liner as the quick start above (it is the quick
start — append anything after `--run` and it reaches `gogogo`):

## Before you push

`make ci-local` locally is the same gate CI runs, and `make signoff` adds a git
stamp. It catches ~95% of regressions in under 3 minutes instead of waiting on a
CI round trip. The tier ladder (T1 format/build → T5 signoff) and what each tier
catches: [local-ci](docs/local-ci.md).

## Contributing

The most valuable contribution right now is **using gogogo for a real
project and reporting what broke** — see [CONTRIBUTING.md](CONTRIBUTING.md).

AI-assisted contributions are welcome, with a human in the loop: disclose
tool use in the PR body (no AI trailers in commits or changelogs), explain
motivation, include a reproduction, keep the diff minimal, and run
`make signoff` before pushing. Please discuss features in an issue before
opening a PR.

## Acknowledgements

Inspired by [northstar](https://github.com/zangster300/northstar) by Zangster —
a Go + NATS + Datastar + Templ + DaisyUI application starter.
See [docs/inspirations.md](docs/inspirations.md) for full credits and learnings.

## License

MIT — see [LICENSE](LICENSE). Open to feedback, PRs, and adaptations. If
something doesn't make sense, if the stack doesn't fit your problem, or if you
have a better idea, [open an issue](https://github.com/calionauta/gogogo/issues).