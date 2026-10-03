# gogogo-fullstack-template

<p align="center">
  <img src="web/resources/static/logo.png" alt="gogogo-fullstack-template" width="420">
</p>

<p align="center">
  <a href="https://calionauta.github.io/gogogo-fullstack-template/">Landing page</a> ·
  <a href="https://calionauta.github.io/gogogo-fullstack-template/docs/">Docs</a> ·
  <a href="https://github.com/calionauta/gogogo-fullstack-template/releases">Releases</a> ·
  <a href="https://github.com/calionauta/gogogo-fullstack-template/actions">CI</a>
</p>

> **Built to be useful.** Every decision favors practical outcomes over abstract
> ideals. The stack optimizes for simplicity, consistency, and shipping software
> with minimal friction.

Every web project starts with the same conversation — pick a database, auth,
router, reactive frontend, task queue… — and the project stalls at the
*decisions*, installations, and configurations. Not at the code. This template
answers those decisions once, ships them wired together, and documents how to
change or remove each piece.

## 📖 Docs (single source of truth)

The manual lives on the site, not in this file:
**[calionauta.github.io/gogogo-fullstack-template/docs/](https://calionauta.github.io/gogogo-fullstack-template/docs/)**

<details>
<summary>Page index (18 pages)</summary>

| Group | Pages |
|---|---|
| **Get started** | [overview](docs/overview.md) · [getting-started](docs/getting-started.md) |
| **Core** | [architecture](docs/architecture.md) · [stack-layers](docs/stack-layers.md) · [async-layers](docs/async-layers.md) · [features](docs/features.md) · [scope-taxonomy](docs/scope-taxonomy.md) · [configuration](docs/configuration.md) |
| **Frontend** | [todo-example](docs/todo-example.md) · [ui-skins](docs/ui-skins.md) · [ui-sounds](docs/ui-sounds.md) |
| **Ship** | [deploy](docs/deploy.md) · [desktop-mobile](docs/desktop-mobile.md) · [admin-dashboard](docs/admin-dashboard.md) · [llm-and-credits](docs/llm-and-credits.md) |
| **Operate** | [local-ci](docs/local-ci.md) · [code-quality](docs/code-quality.md) · [troubleshooting](docs/troubleshooting.md) |

Plus [ARCHITECTURE.md](ARCHITECTURE.md) — the canonical annotated dependency
graph, and [AGENTS.md](AGENTS.md) — the working rules for human and AI agents
in this repo.

</details>

---

## 🚀 Quick start

This repo is a **GitHub template** — click **Use this template** to get your own
copy, then rename it. Do not build inside a clone of the template itself: your
module path would stay `github.com/calionauta/gogogo-fullstack-template` and
deploys would target the template's server directory.

```bash
# 1. Create your repo (or click "Use this template" on GitHub)
gh repo create my-app --template calionauta/gogogo-fullstack-template --clone
cd my-app

# 2. Rename the project — rewrites ~360 references, then builds to prove it
make rename NAME=my-app          # add OWNER=myorg if not under calionauta
make site                        # regenerate the published docs

# 3. Run
make dev
```

Go 1.27+ is the only prerequisite. Open `http://localhost:8080` for the landing
page, then `http://localhost:8080/todo` for the demo (sign in with the seeded
`demo@demo.app` / `demo`).

> `make rename` only rewrites **this** project's identity. Sibling repos under
the same owner (`ai-credits`, `datastar-lint`, `pi-leakguard`) are real
dependencies and are left alone.

> **One `make build` compiles everything.** No build tags, no feature matrix.
> Opt out at runtime with env vars (`NATS_ENABLED=false`, `DAGNATS_ENABLED=false`).

## In the box

| Layer | Choice | Why |
|---|---|---|
| **Language** | Go 1.27 | Fast compilation, easy deploy, lean runtime |
| **Database + auth + API** | [PocketBase](https://pocketbase.io) (embedded SQLite) | Zero-config auth, REST, file storage, and an admin UI at `/_/` — no separate service |
| **Reactive UI** | [Templ](https://templ.guide) + [Datastar](https://data-star.dev) (SSE) + Tailwind v4 | Server-rendered HTML, ~12 KiB client, no JS framework build step |
| **UI skins** | [DaisyUI v5](https://daisyui.com) (default) + [Basecoat](https://basecoatui.com) + [Morpheus](https://github.com/romshark/morpheus) | Three skins compiled into one binary, switchable at runtime |
| **Task queue** | [goqite](https://github.com/maragudk/goqite) + SSE Hub | Background jobs streamed to the browser. No Redis |
| **Durable workflows** | [DagNats](https://github.com/danmestas/dagnats) | Multi-step workflows as declarative JSON; resume after a mid-run kill |
| **Collaboration** | [Loro CRDT](https://github.com/aholstenson/loro-go) + [Rough.js](https://roughjs.com) | Conflict-free state with no last-write-wins data loss |
| **Realtime** | PocketBase realtime + NATS JetStream | Per-user record push, plus cross-instance broadcast |
| **LLM** | [GoAI](https://github.com/zendev-sh/goai) + optional [ai-credits](https://github.com/calionauta/ai-credits) | Any OpenAI-compatible provider; optional billing ledger + BYOK |
| **Secrets** | [age](https://age-encryption.org) + `~/.secrets/` | Local encryption. No vault, no cloud |
| **Desktop / mobile** | [Wails v3](https://wails.io) + [gogpu/ui](https://github.com/gogpu/ui) | Native shells over the same backend, plus a no-HTTP frontend |
| **Linting** | [golangci-lint](https://golangci-lint.run) (27) + [datastar-lint](https://github.com/calionauta/datastar-lint) | Tuned for LLM-authored code: unchecked errors, context leaks, lost context propagation |

Full table with the reasoning behind each choice:
[stack-layers](docs/stack-layers.md).

## Six async layers, one binary

Most templates make you pick **one** async strategy. This one ships six
complementary layers, each with its own opt-out:

```
goqite       → background jobs + SSE hub (always on)
dagnats      → durable multi-step workflows as JSON (DAGNATS_ENABLED=false)
Loro CRDT    → collaborative docs with offline merges (remove internal/collab/)
PB realtime  → record-change push, per-user scoped (always on)
SSE Hub      → ephemeral signals via Datastar protocol (always on)
JetStream    → multi-instance broadcast + cross-instance state (NATS_ENABLED=false)
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

Sign in with `demo@demo.app` / `demo`. The demo's `users` collection is locked —
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
have a better idea, [open an issue](https://github.com/calionauta/gogogo-fullstack-template/issues).

---

Made with intent to be useful, not to be right.