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

## 📖 Docs (single source of truth)

The manual lives on the site, not in this file:
**[calionauta.github.io/gogogo/docs/](https://calionauta.github.io/gogogo/docs/)**

<details>
<summary>Page index (20 pages)</summary>

One-liners mirror the docs site manifest (`site/build.mjs`).

**Get started**
- [overview](docs/overview.md) — what gogogo is, who it is for, and the six async layers it ships.
- [getting-started](docs/getting-started.md) — clone, run, first five minutes, and the commands you need.

**Core**
- [architecture](docs/architecture.md) — directory layout, dependency direction, entry points, and route wiring gotchas.
- [stack-layers](docs/stack-layers.md) — every dependency and why it is in the box.
- [async-layers](docs/async-layers.md) — the six async layers, PB realtime vs SSE Hub, cross-instance and offline sync.
- [features](docs/features.md) — every capability and its runtime opt-out.
- [scope-taxonomy](docs/scope-taxonomy.md) — Core / Plugin / Feature: the rule for deciding what is safe to delete.
- [configuration](docs/configuration.md) — every environment variable and runtime constant.
- [native-zig](docs/native-zig.md) — Go-first policy: why Zig is an exceptional native boundary, plus the agent decision procedure.

**Frontend**
- [todo-example](docs/todo-example.md) — the Todo reference implementation and the contract to imitate.
- [ui-skins](docs/ui-skins.md) — pluggable DaisyUI / Basecoat / Morpheus skins and the plugin contract.
- [ui-sounds](docs/ui-sounds.md) — vendored cuelume sound feedback and its accessibility contract.

**Ship**
- [deploy](docs/deploy.md) — server layout, first-time setup, deploy workflow, build pipeline and version badge.
- [desktop-mobile](docs/desktop-mobile.md) — Wails v3 desktop/mobile, edge sync, and the native window PoC.
- [admin-dashboard](docs/admin-dashboard.md) — admin surfaces, the two-cookie rule, and the DagNats console.
- [llm-and-credits](docs/llm-and-credits.md) — GoAI configuration and the optional ai-credits / BYOK plugin.

**Operate**
- [local-ci](docs/local-ci.md) — the five-tier feedback loop and make signoff.
- [code-quality](docs/code-quality.md) — the 27 linters, how to scope them, and the Datastar-specific rules.
- [troubleshooting](docs/troubleshooting.md) — symptoms and where they actually come from.
- [dagnats-bootstrap-workaround](docs/dagnats-bootstrap-workaround.md) — the upstream DagNats bug behind the trigger console, and the removable seed that works around it.

Plus [ARCHITECTURE.md](ARCHITECTURE.md) — the canonical annotated dependency
graph, and [AGENTS.md](AGENTS.md) — the working rules for human and AI agents
in this repo.

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
(sign in with the seeded `demo@demo.app` / `demo1234456`).

> `make rename` only rewrites **this** project's identity. Sibling repos under
the same owner (`ai-credits`, `datastar-lint`, `pi-leakguard`) are real
dependencies and are left alone.

> **One `make build` compiles everything.** No build tags, no feature matrix.
> Opt out at runtime with env vars (`NATS_ENABLED=false`, `DAGNATS_ENABLED=false`).

## In the box

| Layer | Choice | Why |
|---|---|---|
| **Language** | Go 1.27 | Fast compilation, easy deploy, lean runtime. Go-first: Zig only as an exceptional native escape hatch ([native-zig](docs/native-zig.md)) |
| **Zero-config database, auth and API** | [PocketBase](https://pocketbase.io) (embedded SQLite) | Zero-config auth, REST, file storage, and an admin UI at `/_/` — no separate service |
| **Reactive UI with no JS framework** | [Templ](https://templ.guide) + [Datastar](https://data-star.dev) (SSE) + Tailwind v4 | Server-rendered HTML, ~12 KiB client, no JS framework build step |
| **Three UI skins in one binary** | [DaisyUI v5](https://daisyui.com) (default) + [Basecoat](https://basecoatui.com) + [Morpheus](https://github.com/romshark/morpheus) | Three skins compiled into one binary, switchable at runtime |
| **Background jobs with retry** | [goqite](https://github.com/maragudk/goqite) + SSE Hub | Background jobs streamed to the browser. No Redis |
| **Workflows that survive a restart** | [DagNats](https://github.com/danmestas/dagnats) | Multi-step workflows as declarative JSON; resume after a mid-run kill |
| **Shared state that merges offline edits** | [Loro CRDT](https://github.com/aholstenson/loro-go) + [Rough.js](https://roughjs.com) | Conflict-free state with no last-write-wins data loss |
| **Realtime for one instance or many** | PocketBase realtime + NATS JetStream | Per-user record push, plus cross-instance broadcast |
| **AI features on any provider** | [GoAI](https://github.com/zendev-sh/goai) + optional [ai-credits](https://github.com/calionauta/ai-credits) | Any OpenAI-compatible provider; optional billing ledger + BYOK |
| **Secrets encrypted at rest** | [age](https://age-encryption.org) + `~/.secrets/` | Local encryption. No vault, no cloud |
| **Desktop apps on the same backend** | [Wails v3](https://wails.io) + [gogpu/ui](https://github.com/gogpu/ui) | Native shells over the same backend, plus a no-HTTP frontend |
| **Linting tuned for AI-written code** | [golangci-lint](https://golangci-lint.run) (27) + [datastar-lint](https://github.com/calionauta/datastar-lint) | Tuned for LLM-authored code: unchecked errors, context leaks, lost context propagation |

Full table with the reasoning behind each choice:
[stack-layers](docs/stack-layers.md).

## Everything async your app will need

Six complementary layers, already inside, each with its own opt-out:

```
Background jobs that update the UI   → goqite + SSE Hub (always on)
Workflows that survive a restart     → DagNats as JSON (DAGNATS_ENABLED=false)
Collaboration without data loss      → Loro CRDT (remove internal/collab/)
Live record updates, scoped per user → PB realtime (always on)
Ephemeral UI signals                 → SSE Hub via Datastar (always on)
Broadcast across instances           → JetStream (NATS_ENABLED=false)
```

Records flow through PocketBase's own realtime channel — scoped per user by the
collection's access rules, free of charge. The SSE Hub is reserved for ephemeral
signals (toasts, client counts, workflow progress). Mixing those two up is the
most common mistake in Datastar apps; [async-layers](docs/async-layers.md)
explains why the split matters.

## Capabilities

Every capability is always compiled. Each has a documented opt-out.

| Capability | Opt-out |
|---|---|
| Todo app + PocketBase realtime, stacked toasts, sound feedback | delete `features/todo/` |
| Queue + retry with exponential backoff, SSE `lastRetry` signal | — (core) |
| AI Suggest via GoAI, or keyless simulated LLM for the demo | no `GOAI_API_KEY` and `SIMULATE_LLM=false` |
| AI credits + BYOK relay + Stripe top-ups | `CREDITS_ENABLED=false` |
| Collaborative whiteboard (CRDT + presence + offline outbox) | delete `features/whiteboard/` |
| Durable `WelcomeOnboarding` workflow | `DAGNATS_ENABLED=false` |
| Hybrid offline sync (Service Worker + Leaf Node) | `OFFLINE_SYNC_ENABLED=false` |
| Pluggable persistence (`pb` ⇄ `crdt`) | `ENTITY_STORE=pb` |
| Multi-instance realtime | `NATS_ENABLED=false` |
| Public landing page + auth-gated config view | delete `features/landing/`, `features/config/` |
| Wails v3 desktop/mobile + native window PoC | delete `cmd/desktop/`, `cmd/gui/` |

Infrastructure opts out with an env var. Product features opt out by deleting
the package and its one wiring call — see
[features](docs/features.md).

## Core / Plugin / Feature

Every non-test `.go` file under `internal/` and `features/` carries a SCOPE
annotation on two axes: `layer` (where it lives) and `removal` (what happens if
you delete it). `make check-scope` enforces it in CI and in your pre-commit hook.

| Annotation | Meaning | You would |
|---|---|---|
| `removal=core` 🔴 | The binary does not work without it | Customize, never remove |
| `removal=plugin` 🟡 | The binary works but loses a capability | Swap, or delete + remove the wiring call |
| `removal=feature` 🟢 | A demo or add-on | Keep as reference, then remove |

The rule for agents: **never delete a `removal=core` file without asking; never
keep a `removal=feature` file in production if the domain doesn't need it.**
Full taxonomy and per-package removal table:
[scope-taxonomy](docs/scope-taxonomy.md).

## Try it live

| What | URL |
|---|---|
| **Todo & Whiteboard demo** | [gogogo.calionauta.com](https://gogogo.calionauta.com/) |
| **Live PocketBase admin** | [gogogo.calionauta.com/_/](https://gogogo.calionauta.com/_/) |
| **Durable workflow console (DagNats)** | [gogogo.calionauta.com/dagnats/](https://gogogo.calionauta.com/dagnats/) |

Sign in with `demo@demo.app` / `demo1234456`. The demo's `users` collection is locked —
you can log in as the demo user but cannot create or delete accounts through the
API or the dashboard (only the superuser can).

## Commands

```bash
make dev           # Live reload with Air (regenerates templ + CSS)
make build         # Build binary (unified: everything included)
make ci-local      # Full local gate (= CI): templ, lint, css-check, race tests, build
make signoff       # ci-local + gh signoff stamp — the recommended pre-push gate
make setup         # Activate lefthook git hooks
make site          # Rebuild the docs site from docs/*.md
```

Full command reference: [getting-started](docs/getting-started.md#commands).

## CLI & MCP (agent paths)

Humans get the guided installer above. Agents get the same engine
(`internal/installer`) through two scripted paths — opinions first,
preview second, mutation last:

| Path | Use when | Entry point |
|---|---|---|
| `advise` | deciding what to keep (use-case → keep/drop + off-switches + Go/Zig rule). Changes nothing | `go run ./cmd/gogogo advise --need "..." --format json` |
| trim `--dry-run` | previewing the scaffold as one JSON document | `--no-tui --dry-run --format json` (add `--yes` to apply) |
| `add` / `--check` | restoring a unit into a scaffolded checkout / verifying markers | `add <unit> --from <pristine> --dir <proj> [--dry-run\|--yes]` |
| MCP server | tool calls instead of shell (`advise_stack`, `capabilities_list`, `trim_plan`, `trim_apply`, `check_tree`, `add_unit`) | `cmd/gogogo-mcp` (own module, stdio) + one JSON block in the client config |

Destructive paths refuse without explicit confirmation (`--yes` /
`confirm:true`). Strategy and the foreign-codebase boundary:
[getting-started §0b–0c](docs/getting-started.md#0b-opinions-before-changes-advise).

No Go on this machine? Tagged releases ship static binaries (`gogogo` +
`gogogo-mcp`, checksummed) — and `install.sh` bootstraps a user-space Go
toolchain when none is found (`~/.local/go`, no sudo, existing installs
untouched). Same one-liner as the quick start above (it is the quick
start — append anything after `--run` and it reaches `gogogo`):

## Before you push

`make ci-local` locally is the same gate CI runs, and `make signoff` adds a git
stamp. It catches ~95% of regressions in under 3 minutes instead of waiting on a
CI round trip. The tier ladder (T1 format/build → T5 signoff) and what each tier
catches: [local-ci](docs/local-ci.md).

## Acknowledgements

Inspired by [northstar](https://github.com/zangster300/northstar) by Zangster —
a Go + NATS + Datastar + Templ + DaisyUI application starter.

## License

MIT — see [LICENSE](LICENSE). Open to feedback, PRs, and adaptations. If
something doesn't make sense, if the stack doesn't fit your problem, or if you
have a better idea, [open an issue](https://github.com/calionauta/gogogo/issues).

---

Made with intent to be useful, not to be right.