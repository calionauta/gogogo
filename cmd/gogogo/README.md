# `cmd/gogogo` — guided installer for this template

Scaffold a new project from `gogogo` without hand-rename
drift and without dead code left behind.

```bash
# Interactive (2 questions: name, owner), then hands
# the terminal to `make dev` in the new checkout (Ctrl-C stops dev).
go run github.com/calionauta/gogogo/cmd/gogogo@latest --run

# Scripted (agents must NOT use --run: it never returns)
go run ./cmd/gogogo --name my-app --owner myorg --no-tui
```

## Preflight & codegen (doctor / generate)

Two deterministic entrypoints for LLM-era work (ported from
`mygo doctor` / `mygo generate` pattern). Read-only `doctor`,
deterministic `generate`, no LLM in the path. Full reference:
[`docs/doctor-generate.md`](../../docs/doctor-generate.md).

```bash
go run ./cmd/gogogo doctor --json     # preflight for agents/CI (exit 0 ok / 4 fail)
go run ./cmd/gogogo generate --check  # CI gate: exit 1 on drift
go run ./cmd/gogogo generate          # fix drift yourself (templ+sqlc+css+docs)
```

Checks: `go.version`, `go.tool.templ`, `tool.*` (ripwire/sem/cymbal/xerj/
lefthook/sqlc/docker; `zig` and `mygo` warn opt-in — `mygo` CLI is
packaging only, `make mygo` needs no CLI), `desktop.sdk` (xcode/GTK
warn-only), `mobile.sdk` (skip unless `ANDROID_HOME`), `secrets.age`,
`host.capacity`, `registry.fresh`, `generate.drift` (skip → `generate --check`).

LLM workflow: run `doctor --json` first when starting work; run
`generate --check` before opening a PR; if drift, run `generate` and
commit the regenerated files.

## Recipes

A recipe is a copied slice (code + declaration + receipt), not a
dependency. Discover, install, update, status, invoke and uninstall ride
the same engine the MCP server calls. Full contract:
[`skills/gogogo-recipe-authoring`](../../skills/gogogo-recipe-authoring/SKILL.md).

```bash
go run ./cmd/gogogo recipe install ./_recipes/pomodoro --dir ./my-app --yes
go run ./cmd/gogogo recipe status --dir ./my-app   # pristine / modified / deleted
go run ./cmd/gogogo recipe list --dir ./my-app [--format json]
go run ./cmd/gogogo recipe check todo --dir ./my-app   # evaluator; blocks a route owner with no tests
go run ./cmd/gogogo recipe test todo --dir ./my-app    # the tests THAT recipe ships (scope from its receipt)
go run ./cmd/gogogo recipe invoke todo create --dir ./my-app --param title=hi
```

## What it does, in order

1. Shows the plan with every consequence (never silent).
2. Clones the template into `--dir` when it is missing (asks, or `--yes`
   to proceed unattended).
3. Renames it (same rules as `scripts/rename-project.py`: module path first,
   bare name second, Pages host only on `--owner` change, then every path
   whose NAME carries the template name — `cmd/gogogo`, `cmd/gogogo-mcp`,
   `skills/gogogo-coding-standards` and its `.agents/skills` registration —
   because the content pass rewrites every reference to those paths; shields
   `ai-credits`, `datastar-lint`, `pi-leakguard`; skips generated `site/docs/`).
4. `go mod tidy`, proof `go build ./cmd/web` + the toolchain pin test when
   the tree carries it (it names files by path, so it is what catches a
   half-renamed tree).
5. Writes an `AGENTS.md` in the new repo pointing agents back at the upstream
   template before they add a dependency or a feature.
   Generated code follows `skills/gogogo-coding-standards/SKILL.md`
   (same Go rules `advise` cites).
6. Prints `cd <dir> && make dev` — or, with `--run`, becomes dev itself
   by execing `make dev` in the new checkout (humans only; never returns,
   so agents must not use it).

## Scaffolding one feature inside a project

```bash
# Preview (writes a plan only):
go run ./cmd/gogogo new feature reports --dir ./my-app --dry-run --format json

# Apply: handler + page, chi wiring, capability unit, RBAC rows, then proof.
go run ./cmd/gogogo new feature reports --dir ./my-app --yes
```

`new` only edits scaffolded checkouts (`go.mod` + `internal/server/chi.go` +
the registry + the policy file). It refuses before writing when an anchor
moved, when the name collides with a capability or a reserved route, or when
`features/<name>` already exists. The proof runs `gofmt`, `templ generate`,
the generated-docs regen, registry conformance, `check-flatdeps` and
`go build ./cmd/web`; a failing step exits 2 and leaves the tree for you to
inspect. The generated unit's card appears in `docs/unit-contracts.md`.

A feature that must also answer in Chat adds a `PanelProvider`. Its `Render`
must reuse the feature's own page components (row, card, dialog), never a
chat-specific copy — the contract is `features/genui/components/panel.go`, the
reference is `features/todo/handlers/genui_panel.go`, and the checklist is
`docs/native-panels.md`.

## Desktop sync

`gogogo --help` prints the sync prerequisites (and the MCP server exposes the
same text as `sync_prerequisites`): a desktop build needs a NATS **leaf
listener** the device can reach (`NATS_LEAFNODE_LISTEN`), `NATS_TOKEN` on both
sides, and `CLOUD_SYNC_URL` pointing at the leaf URL. Tailscale/WireGuard is
the lean path; public exposure needs TLS and per-device credentials. Never
point a build at the demo host. Full recipe:
[`docs/desktop-mobile.md#cloud-sync`](../../docs/desktop-mobile.md#cloud-sync).

## Mobile (Android)

The opt-in Android target is `cmd/mobile`; build it with `make mobile` (needs
the Android SDK/NDK, JDK 21 and the pinned Wails CLI). The driver matrix swaps
tursogo for `ncruces/go-sqlite3` behind `android` build tags, and the APK is
local-only (no CI). Prerequisites, limits and the emulator recipe:
[`docs/desktop-mobile.md`](../../docs/desktop-mobile.md#mobile-android-opt-in-target-not-supported).

## MyGo shell

The MyGo peer shell is `cmd/mygo` (loopback webview over `StartChi`, same
flat stack as desktop). Build it with `make mygo` (plain `go build`, no
CLI needed; packaging via `go tool github.com/egoist/mygo/cmd/mygo`
needs the `mygo` CLI — `doctor` check `tool.mygo` is warn-only). Proof,
driver matrix and automation limits:
[`docs/research/mygo-2026-11.md`](../../docs/research/mygo-2026-11.md),
[`docs/desktop-mobile.md#mygo-shell-peer-shell-over-the-flat-stack`](../../docs/desktop-mobile.md).

## CLI contract (humans + agents)

`--help` prints the full matrix (units, flags, exit codes). The fast paths:

```bash
# Humans: 2 questions, plan printed, confirm [y/N], then `make dev`
# takes this terminal (Ctrl-C stops dev).
go run github.com/calionauta/gogogo/cmd/gogogo@latest --run

# Agents, step 1 — preview as JSON (changes nothing, exit 0):
go run ./cmd/gogogo --name my-app --owner myorg \
  --no-tui --dry-run --format json --dir ./my-app

# Agents, step 2 — apply. --yes is mandatory without a terminal:
go run ./cmd/gogogo --name my-app --no-tui --yes --dir ./my-app
```

- `--dry-run` prints the plan (text|json) and stops. `--no-tui` without
  `--yes` also stops after the plan with a re-run hint — the installer never
  deletes on an assumption.
- Scaffold ships every capability. Manual removal is recorded by the
  registry's per-unit `Reason`; there is no keep/drop prompt or preset.
- `--format json` field names are stable (`name`, `owner`, `module`, `dir`,
  `keepUnits`, `drop` (the unit lists, stable).
- Exit codes: 0 ok or plan-only, 1 usage/validation/apply error, 2 the
  post-rename proof build failed (compiler output included — fix the named
  file, don't guess).

## Alternatives considered

| Option | Verdict |
|---|---|
| `curl \| bash` shell script | Adopted as **distribution only**: `install.sh` fetches checksummed release binaries (thin wrapper, zero logic). Rejected as the trim engine itself: fragile TUI, hard to safely edit Go wiring, easy to half-trim. |
| Extend `scripts/rename-project.py` with trim flags | Rejected: Python is not guaranteed on the user's machine, no TUI, and the trim engine belongs next to the Go code it edits. The Python script stays as the rename reference + `make rename` fallback. |
| Go TUI with Charm `Huh` + `Bubbletea` in a **separate module** | Right UX, right stack (2026-standard, performative), but adds deps. Deferred: the prompts here are structured as a form model so they can move to `Huh` without touching the trim engine. When that happens it must live in its own `go.mod` so the main binary stays lean. |
| `go run ./cmd/gogogo` stdlib-only in the main module (this) | **Chosen**: `go run` needs nothing but Go, zero new dependencies, fully testable, and the trim engine is the same one a future TUI would call. `--no-tui` flag shape already matches the scripted path. Prebuilt binaries (GoReleaser, same engine) cover Go-less machines. |
| `gh extension` / `make init` wrapper | Rejected as primary (needs `gh` + extension install); `make init` may wrap this CLI later for discoverability. |

## Manual deletions (registry paths)

Deletion is manual: each capability's `Reason` in
`internal/capabilities` says what disappears with the unit.
The table below is the per-unit manual deletion map — use it when a user
wants a smaller tree, guided by the unit card in `docs/unit-contracts.md`.

| Unit | Deletes | Manual edit |
|---|---|---|
| `dagnats` | `internal/dagnats/`, `features/todo/handlers/onboarding.go` (+ its test), `internal/nats/single_nats_test.go`, `cmd/web/dagnats.go`, `cmd/web/start_nats_test.go` | Drop the `bootOnboarding` line and the `/api/onboarding` mounts in `internal/server/chi.go`; `startDagNats` / `shutdownDagNats` calls in `cmd/web/main.go`; drop `danmestas/dagnats` from `go.mod`. No dead UI: the workflow tab auto-hides via the `dagnatsUIEnabled` signal |
| `goakt` | `internal/goakt/`, `features/room/`, `cmd/web/goakt.go` | Drop the `bootRoomActor` line, the `/room` mounts and the room-actor shutdown in `internal/server/chi.go` (+ the `fakeRooms` fixture in its test); `startGoAkt` / `shutdownGoAkt` calls in `cmd/web/main.go`; drop `tochemey/goakt` from `go.mod`. Navbar Room link stripped |
| `whiteboard` | `features/whiteboard/`, `features/notes/`, `internal/collab/` | Drop the `wbhandlers`/`noteshandlers` handler lines and the `/whiteboard` + `/notes` mounts in `internal/server/chi.go`; Phase C demo block in `cmd/desktop/main.go` (+ `collab`/`context`/`time` imports); Whiteboard + Notes `[]Module` entries stripped |
| `landing` | `features/landing/` | Drop the `landinghandlers.NewHandler` line, its import and the `/` mount in `internal/server/chi.go`; shell brand retargeted `/` → `/todo`. Warns: plain GET / 404s afterwards |
| `config-view` | `features/config/` | Drop the `confighandlers.NewHandler` line, its import and the `/config` mounts; Config `[]Module` entry stripped |
| `credits` | `features/credits/` | Drop the `bootCredits` line and the `/api/credits` + `/api/ai` + `/api/byok` mounts in `internal/server/chi.go`; drop `ai-credits` (+ `stripe-go` if unused) from `go.mod`. Warns: AI Suggest becomes unmetered |
| `sounds` | `features/sounds/`, `web/resources/static/cuelume.js`, `web/resources/static/cuelume/` | `@sounds.*` calls + `features/sounds` imports stripped from all 5 page layouts (whichever remain); `go tool templ generate` re-runs automatically. Warns: `data-cuelume-*` attrs stay, inert |
| `genui` | `features/genui/`, `features/chat/` | Drop the `panels` line and the `Panels` Deps field in `internal/server/chi.go`; Chat `[]Module` entry stripped; mux target table shrinks (re-add by hand, see follow-ups). Warns: `SMOKE_FEATURES` must drop `genui` |
| `review` | `features/review/` | Drop the `bootReview` helper + `reviewH` line and the `/review` + `/api/review/` mounts in `internal/server/chi.go` (+ the `newReviewHandlerForTest` fixture in its test); Review `[]Module` entry stripped; `go mod tidy` drops `genkit` + `go-jev`. Warns: `SMOKE_FEATURES` must drop `review` |
| `search` | `internal/search/` | Delete the package and the capability entry. A consumer keeps working with the SQL-only substring layer (it holds a `Searcher`), and `features/todo/handlers/search_source.go` goes with the unit because it exists only to feed the plugin |
| `perf` | `internal/perf/`, `internal/server/perf.go` (+ its test), `internal/ui/perf.go` | Drop the `bootPerf`/`closePerf` lines and the `perfPageMiddleware`/menu entry; remove `PerfEnabled` and the `/debug/*` policy rows; drop `arl/statsviz` from `go.mod`. Manual deletion; the runtime switch keeps it off in production |

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
| Entity actors (GoAkt rooms) | `GOAKT_ENABLED=false` | `internal/goakt/` + room demo | ✅ `goakt` |
| Realtime (NATS JetStream) | `NATS_ENABLED=false` (in-memory fallback) | Manual — 15+ importers (`internal/nats` in handlers, collab, server, desktop); handler signatures carry NATS types | ❌ env-blessed (cascade too wide for line strips) |
| Collaboration (Loro CRDT) | — (no servers at boot; `COLLAB_SNAPSHOTS=memory` for snapshot-free local mode) | Bundled with `whiteboard` (see bundle rationale in manifest) | ✅ inside `whiteboard` |
| Todo realtime (SSE Hub) | — (part of the Hub) | With `features/todo/` (manual, it is the reference) | ❌ |
| LLM suggest (GoAI) | Unset `GOAI_API_KEY` (button hides) | Manual — handler + stepper UI live inside `features/todo` | ❌ env-blessed |
| AI credits + BYOK | `CREDITS_ENABLED=false` (default) | `features/credits/` + wiring | ✅ `credits` |
| Generative UI + chat | Unset `GOAI_API_KEY` (chat stub in tests) | `features/genui/` + `features/chat/` + wiring | ✅ `genui` |
| Review loop + typed AI flows | Unset `GOAI_API_KEY` (page shows setup hint; run 503s) | `features/review/` + wiring (Genkit bundled inside `review`) | ✅ `review` |
| Offline sync | `OFFLINE_SYNC_ENABLED=false` | Manual — `config` struct field + `sw.js` + templ SW registration | ❌ env-blessed (struct-field cascade) |
| Persistence (single Turso/sqlc store) | — (core, no switch) | — (core data layer, not trimmable) | ❌ |
| Auth | — (core middleware) | Login UI removable manually; middleware stays | ❌ |
| Sounds | — (client-side only) | `features/sounds/` + call sites | ✅ `sounds` |
| Operator diagnostics (statsviz + pprof) | `PERF_ENABLED=false` | `internal/perf/` + mount/menu/config | ✅ opt-in `perf` |
| Landing / config view | — (zero boot cost) | Package + wiring call | ✅ `landing`, `config-view` |

Full env reference: `docs/configuration.md`. Deletion checklists:
`docs/scope-taxonomy.md#removing-a-component`.

After any manual deletion, run `go mod tidy` and `go build ./cmd/web` as
proof. If the build fails it prints the compiler error verbatim — fix the named
file, don't guess.

## Upstream-first rule (generated projects)

Every scaffolded repo gets an `AGENTS.md` section requiring agents to check the
upstream template **before** adding a dependency or a feature: fetch
`site/llms.txt` (map) and the relevant `site/docs/<slug>/` page, or the GitHub
blob for non-docs files, and reuse what exists instead of inventing a parallel
implementation. See `agents.go` for the exact text.
