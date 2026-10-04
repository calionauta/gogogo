# `cmd/gogogo` — guided installer for this template

Scaffold a new project from `gogogo` without hand-rename
drift and without dead code left behind.

```bash
# Interactive (4 questions: name, owner, plugins, features), then hands
# the terminal to `make dev` in the new checkout (Ctrl-C stops dev).
go run github.com/calionauta/gogogo/cmd/gogogo@latest --run

# Scripted (agents must NOT use --run: it never returns)
go run ./cmd/gogogo --name my-app --owner myorg \
  --plugins dagnats,nats,llm --features todo,landing,config --no-tui
```

## What it does, in order

1. Shows the trim plan with every consequence (never silent).
2. Deletes skipped plugins/features with their wiring calls.
3. Renames it (same rules as `scripts/rename-project.py`: module path first,
   bare name second, Pages host only on `--owner` change; shields `ai-credits`,
   `datastar-lint`, `pi-leakguard`; skips generated `site/docs/`).
4. Trims what you skipped (see below), then `go mod tidy`, `make templ`,
   proof `go build ./cmd/web`.
5. Writes an `AGENTS.md` in the new repo pointing agents back at the upstream
   template before they add a dependency or a feature.

## CLI contract (humans + agents)

`--help` prints the full matrix (units, flags, exit codes). The fast paths:

```bash
# Humans: 4 questions, plan printed, confirm [y/N], then `make dev`
# takes this terminal (Ctrl-C stops dev).
go run github.com/calionauta/gogogo/cmd/gogogo@latest --run

# Agents, step 1 — preview as JSON (changes nothing, exit 0):
go run ./cmd/gogogo --name my-app --owner myorg \
  --plugins dagnats,whiteboard --features landing,config-view \
  --no-tui --dry-run --format json --dir ./my-app

# Agents, step 2 — apply. --yes is mandatory without a terminal:
go run ./cmd/gogogo --name my-app --no-tui --yes --dir ./my-app
```

- `--dry-run` prints the plan (text|json) and stops. `--no-tui` without
  `--yes` also stops after the plan with a re-run hint — the installer never
  deletes on an assumption.
- `--check --dir X` verifies every marker against a checkout without
  changing anything (drift gate; `CHECK-OK`/`CHECK-FAIL` lines, exit 1 on
  drift). The same check runs in-process as `TestCheckTreeAgainstRepoRoot`,
  so CI fails when a source edit moves a marker.
- `add <unit> --from TEMPLATE --dir PROJECT` reverses a trim (or ports a
  unit into an evolved codebase): resolves `DependsOn` first (e.g. adding
  whiteboard pulls sounds), copies owned paths, re-inserts call lines +
  imports + spans at the anchor table, rebases the template module path
  to the target's, then the same tidy+build proof. Imports restore only
  where used (no unused-import breakage). `.templ` call sites and the
  navbar brand stay manual (warned). Refuses non-scaffolded trees.
  Refuses non-scaffolded trees instead of guessing.
- Interactive menus are numbered (stable manifest order) and accept
  numbers, ids, or both; invalid answers re-ask. Scripted flags take ids
  only — numbers never cross the CLI boundary (positional = fragile).
- `--format json` field names are stable (`name`, `owner`, `module`, `dir`,
  `keepPlugins`, `keepFeatures`,
  `drop[{id, kind, dirs, files, warnings, note, wiringStrips}]`).
- Exit codes: 0 ok or plan-only, 1 usage/validation/apply error, 2 the
  post-trim proof build failed (compiler output included — fix the named
  file, don't guess).

## Alternatives considered

| Option | Verdict |
|---|---|
| `curl \| bash` shell script | Adopted as **distribution only**: `install.sh` fetches checksummed release binaries (thin wrapper, zero logic). Rejected as the trim engine itself: fragile TUI, hard to safely edit Go wiring, easy to half-trim. |
| Extend `scripts/rename-project.py` with trim flags | Rejected: Python is not guaranteed on the user's machine, no TUI, and the trim engine belongs next to the Go code it edits. The Python script stays as the rename reference + `make rename` fallback. |
| Go TUI with Charm `Huh` + `Bubbletea` in a **separate module** | Right UX, right stack (2026-standard, performative), but adds deps. Deferred: the prompts here are structured as a form model so they can move to `Huh` without touching the trim engine. When that happens it must live in its own `go.mod` so the main binary stays lean. |
| `go run ./cmd/gogogo` stdlib-only in the main module (this) | **Chosen**: `go run` needs nothing but Go, zero new dependencies, fully testable, and the trim engine is the same one a future TUI would call. `--no-tui` flag shape already matches the scripted path. Prebuilt binaries (GoReleaser, same engine) cover Go-less machines. |
| `gh extension` / `make init` wrapper | Rejected as primary (needs `gh` + extension install); `make init` may wrap this CLI later for discoverability. |

## Safe trim subset

The installer only deletes what it can decouple without breaking the build.
Capability metadata (kinds, warnings, owned paths, runtime switches) comes
from `internal/capabilities` — the single source of truth, enforced by
conformance tests on both sides. The tables below are human prose over that
registry: unit ids must match it (`TestManifestUnitsMatchRegistry`) and
appear here (`TestReadmeDocumentsUnits`).

| Unit | Deletes | Router / wiring strip |
|---|---|---|
| `dagnats` | `internal/dagnats/`, `router/onboarding_dagnats.go`, `router/dagnats_proxy_dagnats.go`, `router/dagnats_proxy_test.go`, `router/export_test.go`, `features/todo/handlers/onboarding.go`, `features/todo/handlers/todo_update_job_dagnats.go`, `features/todo/handlers/onboarding_resume_test.go`, `features/todo/onboarding_e2e_test.go`, `internal/nats/single_nats_test.go`, `cmd/web/dagnats.go`, `cmd/web/start_nats_test.go` | Drop 1 call line in `router/router.go` (guard lives inside `registerOnboarding`); `startDagNats` / `shutdownDagNats` calls in `cmd/web/main.go` (kept as `_ = todoH` so nothing dangles); drop `danmestas/dagnats` from `go.mod`. No dead UI: the workflow tab auto-hides via the `dagnatsUIEnabled` signal |
| `whiteboard` | `features/whiteboard/`, `internal/collab/`, `router/whiteboard.go`, `router/collab_jetstream.go` | Drop 1 call line (`registerWhiteboardStack`); Phase C demo block in `cmd/desktop/main.go` (+ `collab`/`context`/`time` imports); navbar Whiteboard link stripped. `loro-go` stays: `features/store/crdtstore` still imports it |
| `landing` | `features/landing/` | Drop 1 call line + `landing` import in `router/router.go`; navbar brand retargeted `/` → `/todo`. Warns: plain GET / 404s afterwards |
| `config-view` | `features/config/` | Drop 1 call line + `cfgfeature` import; navbar Config link stripped |
| `credits` | `features/credits/`, `router/credits.go` | Drop 1 call line (`wireCredits`); drop `ai-credits` (+ `stripe-go` if unused) from `go.mod`. Warns: AI Suggest becomes unmetered |
| `sounds` | `features/sounds/`, `web/resources/static/cuelume.js`, `web/resources/static/cuelume/` | `@sounds.*` calls + `features/sounds` imports stripped from all 5 page layouts (whichever remain); `go tool templ generate` re-runs automatically. Warns: `data-cuelume-*` attrs stay, inert |
| `skins-extra` | `web/skins/basecoat/`, `web/skins/morpheus/` | Blank imports dropped from `features/todo/components/skin_imports.go`; morpheus/basecoat dispatch branches + imports stripped from `features/todo/handlers/todo.go` + `todo_repo.go` (DaisyUI default stays). Warns: `?skin=` falls back to DaisyUI; static bundles stay embedded (manual follow-up with the `css-basecoat` target) |

Deliberately **not** offered: `todo` (reference implementation — remove manually
later per `docs/scope-taxonomy.md`), `auth` (middleware is core), `queue`
(core), `nats` core (runtime-disable with `NATS_ENABLED=false` instead),
`llm` (runtime-disable by unsetting `GOAI_API_KEY` instead — the suggest
handler lives inside `features/todo` and would dangle).

## Capability map: runtime off vs deletion

Two complementary mechanisms, one rule: **boot-costly capabilities get env
switches; zero-boot-cost product surface gets deletion.** The installer only
offers deletion where it is mechanical and proven; everything else is
env-blessed with the reason recorded.

| Capability | Runtime off (reversible) | Deletion (permanent) | Installer |
|---|---|---|---|
| Durable workflows (DagNats) | `DAGNATS_ENABLED=false` | `internal/dagnats/` + onboarding wiring | ✅ `dagnats` |
| Realtime (NATS JetStream) | `NATS_ENABLED=false` (in-memory fallback) | Manual — 15+ importers (`internal/nats` in handlers, collab, server, desktop); handler signatures carry NATS types | ❌ env-blessed (cascade too wide for line strips) |
| Collaboration (Loro CRDT) | — (no servers at boot) | Bundled with `whiteboard` (see bundle rationale in manifest) | ✅ inside `whiteboard` |
| Todo realtime (PB) | — (part of the DB) | With `features/todo/` (manual, it is the reference) | ❌ |
| LLM suggest (GoAI) | Unset `GOAI_API_KEY` (button hides) | Manual — handler + stepper UI live inside `features/todo` | ❌ env-blessed |
| AI credits + BYOK | `CREDITS_ENABLED=false` (default) | `features/credits/` + wiring | ✅ `credits` |
| Offline sync | `OFFLINE_SYNC_ENABLED=false` | Manual — `config` struct field + `sw.js` + templ SW registration | ❌ env-blessed (struct-field cascade) |
| UI skins | `UI_SKIN=daisyui` / `?skin=` | `web/skins/<name>/` + dispatch branches | ✅ `skins-extra` (non-default skins) |
| Persistence strategy | `ENTITY_STORE=pb\|crdt` | Manual — `buildTodoStore` switch + `ConcreteTodoStore` machinery | ❌ env-blessed |
| Auth | — (core middleware) | Login UI removable manually; middleware stays | ❌ |
| Sounds | — (client-side only) | `features/sounds/` + call sites | ✅ `sounds` |
| Landing / config view | — (zero boot cost) | Package + wiring call | ✅ `landing`, `config-view` |

Full env reference: `docs/configuration.md`. Deletion checklists:
`docs/scope-taxonomy.md#removing-a-component`.

After any trim the installer runs `go mod tidy` and `go build ./cmd/web` as
proof. If the build fails it prints the compiler error verbatim — fix the named
file, don't guess.

## Upstream-first rule (generated projects)

Every scaffolded repo gets an `AGENTS.md` section requiring agents to check the
upstream template **before** adding a dependency or a feature: fetch
`site/llms.txt` (map) and the relevant `site/docs/<slug>/` page, or the GitHub
blob for non-docs files, and reuse what exists instead of inventing a parallel
implementation. See `agents.go` for the exact text.
