# gogogo

> Full-stack Go web app template — back-end + front-end + DB + auth + LLM + deploy in one binary.

## Project Overview

Go template: Datastar + Templ + Turso (sqlc) + owned auth + goqite + DagNats + NATS JetStream. Module: `github.com/calionauta/gogogo`

**Naming:** repo, module, binary, deploy dir (`/home/deploy/<APP_NAME>/`), container, tunnel hostname share the project name. Fresh clones carry `gogogo` in ~670 places across ~200 files — never rename by hand (run `python3 scripts/rename-project.py --dry-run my-app` for the current count). Use `go run ./cmd/gogogo` (guided; see `cmd/gogogo/README.md`) or `make rename NAME=my-app [OWNER=myorg]`.

**Upstream-first (generated projects keep this):** check the upstream template (`site/llms.txt` + `site/docs/<slug>/`, or the GitHub blob) BEFORE creating a feature or installing a library. Reuse what exists (Todo is the reference; `docs/unit-contracts.md` lists what each unit already owns). Scaffold a new feature with `go run ./cmd/gogogo new feature <name>` (wiring + registry + RBAC rows + proof) instead of copying a slice by hand. New dependency only when no upstream page covers the need.

**Unified build.** `go build ./cmd/web` compiles everything, no build tags. Opt out at runtime (`NATS_ENABLED=false`, `DAGNATS_ENABLED=false`, `GOAKT_ENABLED=false`). Backup: `scripts/backup.sh` (+ `restore.sh --dry-run` drill); retention per store: `docs/async-layers.md#durability-and-retention-per-store`.

## Stack (exact versions)

Go 1.27 | Templ v0.3.1020 | Datastar v1.0.4 (JS core, vendored) + datastar-go v1.2.2 (Go SDK) | Turso (tursogo v0.8.3, sqlc queries) | TailwindCSS v4.3.3 + DaisyUI v5.7.42 | goqite v0.4.0 | retry-go v4 | DagNats v0.0.30 | GoAkt v4.6.1 (room lease + whiteboard presence) | NATS JetStream | age v1.3.2 | uuid v1.6.0

## Skills

- **Load them, don't paraphrase them.** Skills are registered for agent hosts at `.agents/skills/<name>` (symlinks to `skills/<name>`), so a host can open them BY NAME — read the SKILL.md before the first matching edit of a session, not after a gate fails. `cmd/check-skill-frontmatter` (pre-commit + CI) fails when a skill is unregistered or a link dangles; nothing else enforces that they were read.
- `skills/gogogo-coding-standards` — Go + template rules (concurrency, perf, testing, Datastar, exception-to-go gate). Use when editing `.go`, spawning goroutines, wiring context, running lint/tests, profiling, or proposing native code. On a `.templ` edit it fires together with `gogogo-ui`: it owns the Datastar/backend gate, `gogogo-ui` owns the vocabulary and the CSS. Universal principles live in the vendored `skills/stelow-workflow-coding-standards` (upstream [stelow](https://github.com/calionauta/stelow), pinned in its `UPSTREAM_SHA` — never edit the vendored copy in place). Drift: `bin/check-stelow-drift.sh` detects, `scripts/refresh-stelow.sh` fixes (a scheduled workflow opens the bump PR).
- `skills/gogogo-ui` — template UI rules (DaisyUI vocabulary, semantic colors, compiled `app.min.css`). MUST read `skills/gogogo-ui/SKILL.md` plus `docs/ui-themes.md` before any `.templ`/CSS edit. The component vocabulary itself is the vendored `skills/daisyui` skill (byte-identical to `https://daisyui.com/SKILL.md`, pinned in its `UPSTREAM_SHA` — never edit the vendored copy in place). Drift: `bin/check-daisyui-drift.sh` detects, `scripts/refresh-daisyui.sh` fixes.
- `cali-code-navigation` — ripwire orient-first navigation. Use when landing cold in unfamiliar code or tracing callers.
- `/skill:cali-ops-deploy-github-tailscale` — server layout, deploy user, secret tables.
- **Agent tooling (install once per machine, index per repo — enforced by `make check-agent-tools`, runs in `make setup`):** `ripwire` (required, orient-first — install via `redhat-et/ripwire`, no daemon, no index step, call via bash); `cymbal` (required — `brew install cymbal`, then `cymbal index .` once per repo, auto-refresh after); `xerj` (required for docs/prose — `curl -fsSL https://xerj.org/get | sh`, then `xerj --insecure --data-dir ~/.xerj-data &` + `xerj autoindex .`, MCP via `xerj mcp`; keep its data dir OUTSIDE the repo, never commit indexes); `sem` (required — `brew install sem-cli`, no setup, cache outside the repo; if `sem --version` prints GNU parallel, put brew/cargo bin first in PATH).
- **Tool decision rule:** symbol/call-graph questions (who calls X, blast radius, tests to run, diff impact) → ripwire first (`ripwire . --for="<task>"`), cymbal to drill (`investigate`/`trace`/`impact`/`changed`). What changed / history / review evidence → `sem diff` (entities, not lines), `sem history` (blame, hotspots, co-change), `sem certify` (range certificate). Meaning/concept questions over code AND prose (docs audits, stale/contradiction sweeps, reference coding, cross-session memory) → xerj — cymbal/ripwire don't read prose.
- **Docs rule:** after ANY behavior change, sweep `docs/` + the site map (`site/docsite.config.mjs` MANIFEST) for staleness vs the diff — concept sweeps via xerj, exact-claim checks via grep/`sem` (verified: xerj lexical retrieval underperforms both on this corpus) — and confirm with `sem diff` (it diffs Markdown by heading — proves docs moved with code), update the doc in the SAME commit, verify with `make site-check` (docs-staleness hook stays advisory). **Never write a count by hand** (a hand-written capability count drifted from 24 to 35): counts live only in the generated `docs/unit-contracts.md#registry-at-a-glance`, and `make check-docs` rejects a hardcoded capability/slice/unit count, a dead repo path, an invented `gogogo` subcommand, and any page under `docs/` that is neither published by the site MANIFEST nor declared repo-facing (closed list: `docs/migrations/`). Changelog entries are release-please's job — it GENERATES them from conventional commits (never write `[Unreleased]` narrative; nothing promotes it).
- **Where a doc goes:** an evaluation, comparison or tradeoff — including options that never shipped — belongs in `docs/research/`: title + `Reviewed: YYYY-MM-DD` (or `living`), a row in the hub `docs/research/index.md`, a MANIFEST entry, then `make docs-index`. The decision record (`docs/architecture-decision.md`) keeps the choice, the evidence and what it replaced, stamped `Status: accepted`/`revised`/`superseded by <link>` with dates, and links to the hub instead of arguing the alternative in place.

## Commands

| Command | Description |
|---------|-------------|
| `make dev` | Air live reload |
| `make build` | Unified build, no lint/tests |
| `make desktop` / `make gui` / `make mobile` | Native targets (Wails shell, gogpu/ui window, Android APK) — built **locally, never in CI**; `make run-desktop` / `make run-gui` build then open; mobile installs with `adb` (`docs/desktop-mobile.md`) |
| `make templ` | Generate Templ |
| `make datastar-lint` | Lint `.templ` (`-only-errors` keeps intentional attrs) |
| `make css` / `make css-check` | Rebuild the bundle (DaisyUI + its themes; scans `features/`, `web/`, `internal/`) / verify it is current (`css-all` is kept as an alias for CI and Docker) |
| `make check-scope` | Assert `// SCOPE:<core|plugin|feature>` on every `internal/`+`features/` file |
| `make test` | Race tests, parallel across packages (CI runs them — prefer scoped tests locally) |
| `make check-generated` | Fail if committed `_templ.go` does not match the `.templ` sources |
| `make ci-local-fast` | Same cheap checks, but lint + tests scoped to changed packages |
| `make lint-safe` | Host-aware lint: sizes to free RAM/cores, scopes to changed pkgs when tight, memory-capped in a cgroup (`scripts/lint-safe.sh`) — prefer over `make lint` on a shared host |
| `make ci-local` | **Single gate** (= CI): checks + lint + build + boot smoke + parallel fan-out (race suite + browser smokes, one log each). If green, push. (`make check` was removed — redundant subset.) |
| `make signoff` | `ci-local` + advisory `gh signoff` stamp before push |
| `make setup` | Activate lefthook git hooks (`core.hooksPath=.githooks`) |
| `make go-bump` | Move the pinned Go toolchain to a newer **patch** of the current minor line, in every file that names it (`DRY=1` reports, `TO=x.y.z` names the target; a minor bump is refused — human decision). `go-auto-bump.yml` runs the same command weekly and opens a reviewed PR |
| `go run ./cmd/gogogo new feature <name>` | Scaffold one feature: handler + page, wiring, capability unit, RBAC rows, then gates + build proof (card in `docs/unit-contracts.md`) |

## Don'ts

- NO HTMX/Alpine (Datastar), NO `fmt.Sprintf` HTML (Templ), NO `log` (`log/slog`).
- Driver is platform-selected (tursogo/`turso` on web/desktop, ncruces/`sqlite3` on `cmd/gui`/Android) over one schema; handlers call `dbgen.*` directly. NO removing goqite for JetStream; different problems.
- NO hand-rolled IDs on rows — use `dbschema.NewID()` (UUIDv7 string, matches every owner column).
- NO authorization by ad-hoc `role == "admin"` in a handler (ruleguard `RoleCompareOutsideRBAC` fails CI) — route decisions go in `casbin/policy.csv`, per-record decisions in `canDeleteDoc`/`canDeleteRow`. NO shared-doc read/write without a session check in the handler: `RBAC_ENABLED` defaults to true in production (false in development), and the policy is inert only if an operator opts out explicitly (`docs/rbac.md`).
- NO `PatchElements` without top-level `id` + `WithSelector` (`PatchElementsNoTargetsFound`). Use `internal/datastar.RenderAndPatch`.
- NO real LLM in tests — inject a stub (`internal/llm/fakeserver` only inside `internal/llm/`).
- NO secrets in git, chat-visible commands, or test fixtures — keys live in `~/.secrets/` (600) or age `.env.age` via `bin/init-secrets`; `gitleaks` pre-commit + CI enforce it. Never print secret values (lengths/names only).
- NO agent self-certification in-loop — tests guarding new behavior follow RED/GREEN/PROVE (acceptance from human/fresh context → implement → hand-mutate, watch fail, revert; never commit the mutation); no mandated TDD red-green cycle (evidence: ProgramBench + DeepSWE 2026). Normative text: `skills/gogogo-coding-standards/references/go-testing.md`.
- Prefer Datastar attributes over vanilla JS; inline JS only adjacent to the markup.
- NO new direct dependency without passing `docs/dependency-principles.md` (write it or rent it) and leaving the one-line trade-off record in the PR body; raising the native floor (a new CGO/native library, build tool, or runtime library) or substituting a core path is a human gate, never an agent decision.
- NO `go install <tool>@latest` for the gates — external tools are pinned by the `tool` directive in `go.mod` (`templ`, `deadcode`, `govulncheck`) and run as `go tool <name>`; `internal/gate_tools_test.go` fails CI on a floating install or a bare binary from `PATH`. An unpinned tool can be older than the Go toolchain, which is how `deadcode` once reported type-check errors as dead code, and a scanner fetched at gate time is an unpinned attack surface inside the step that decides whether a push is safe. Add one with `go get -tool <pkg>@<version>`.
- NO `make check`, NO whole-repo `golangci-lint run ./...` for small changes (scope to touched pkgs — or just `make lint-safe`, which sizes the run to the host's free RAM/cores and caps it in a cgroup), NO `golangci-lint cache clean` (the cache is what keeps a run cheap; a cold run re-type-checks the whole module and spikes memory — it has taken a shared host into swap), NO `go build -tags "<stale>"` (unified-build era has no tags). Project-specific footguns go in `rules/rules.go` (ruleguard, loaded by gocritic) — a rule there fails CI, so prose guidance that a linter can enforce belongs there, not in this file.
- NO `go func()` loop without a shutdown path: long-lived loops select on `ctx.Done()` (or a `done chan struct{}`), bound at wiring time. NO `ctx` stored in a struct. NO bare `for range ticker.C`. See `skills/gogogo-coding-standards` (Concurrency) + [docs/code-quality.md](docs/code-quality.md#concurrency-and-resources).
- NO `--no-verify` commits to dodge a red gate — a red hook is a finding about your change, not an obstacle. Fix it, narrow the scope, or drop the change. (Precedent: a funlen breakage shipped past local hooks this way and failed CI instead.)
- NO legacy until 1.0 (only the human declares 1.0, never an LLM): no backward-compat code, no fallback branches for removed shapes, no deprecated aliases, no `legacy*` helpers, no comments explaining current behavior by reference to removed code — delete the old path, do not bridge it. Reword history-leaning comments to present tense. Exceptions (the only ones): DB migrations stay append-only (data loss beats code purity), and decision records (`docs/architecture-decision.md`, CHANGELOG) keep history — recording decisions is their job.
- After manual smoke/debug runs: `make smoke-clean` — binaries and tmp dirs survive failures and interrupts (the suite only removes them on the happy path). Test artifacts live in the OS temp dir by construction (`SMOKE_BIN`, never `/tmp`-hardcoded); never leave binaries in the repo (a 44MB binary was once committed by accident). Never delete Playwright browsers in cleanup (per-OS shared cache, GBs to re-download).

## Go-first / Zig (summary; normative: `docs/exception-to-go.md`)

Go is the product language (~95-99%). Zero Zig code/toolchain in tree — do not add without a benchmarked case. Bans: "Zig is faster", manual memory, "low-level", "could be optimized", "looks like SIMD", preference, avoiding a Go dep, future perf. Go SIMD (`simd`/`archsimd`, `GOEXPERIMENT=simd`) first; perf work needs a Go baseline bench + profile proving Go is the bottleneck. Kernel rules: one package, small C ABI, caller-owned buffers, pure-Go fallback day one, removable in minutes. Vague "use Zig when appropriate" is rejected — follow the doc's decision procedure. **A native UI in Zig = [`nanoui`](https://github.com/justrach/nanoui)** (immediate-mode, Metal/CPU-rasterizer, ~0.7 MB, MIT) as a SEPARATE binary like `cmd/desktop` — never a Zig kernel inside `cmd/web`; pin its release tag.

## Architecture seams

Keep architecture explicit, thin, and easy for LLMs to evolve. Avoid creating a framework.

**Database driver by target.** tursogo (`turso`, Turso Database) for `cmd/web` and `cmd/desktop`; `ncruces/go-sqlite3` (`sqlite3`) for `cmd/gui` (tursogo's purego/fakecgo collides with goffi at `CGO_ENABLED=0`) and `cmd/mobile` (tursogo embeds no Android library). One schema, migrations and sqlc queries; see `docs/stack-layers.md#database-driver-by-target`.

**Principles (LLM-friendly, evolvable; 2026 practice):**
- **Thin seams over clever abstractions.** Add interfaces only when you actually have 2+ implementations or a real need to swap. Prefer small, purpose-built interfaces (1-3 methods). Never reach for `GenericRepo[T]`. Go's implicit interfaces mean no ceremony.
- **Direct calls over interfaces.** Call the concrete dependency (a sqlc function, a chi handler) directly. Reserve an interface for a genuinely swappable edge (e.g. an LLM provider) and keep it to 1-3 methods. A one-implementation interface is a shallow module — delete it.
- **Explicit DI, no containers.** Inject via struct fields or constructor args. No reflection-based DI, no service locator.
- **Vertical slices > horizontal layers.** Implement end-to-end for a single feature (red-proof with contract tests). Generalize only when the third concrete case repeats. This maximizes locality for LLMs.
- **No pass-through layers.** Never wrap a dependency in a thin adapter that only forwards calls — call it directly. Translate types at the boundary only when the external shape genuinely differs from the domain shape.
- **Examples are the spec.** Living code is more useful to LLMs than long prose. When adding a new seam, mirror an existing concrete example (e.g. the todo vertical slice).
- **Dual-mode when evolving core paths.** When replacing/swapping a core integration, use a runtime flag to compare behavior with zero regressions. Default to existing behavior.
- **Core vs plugins.** Mark new removable pieces with `SCOPE:plugin`. `SCOPE:core` changes need care.
- **Constructors build, callers start; stoppable on terminate.** Follow concurrency rules from coding standards.
- **Fail fast, explicit errors.** Keep contracts obvious; prefer compile-time checks over runtime magic.

**Rules:**
- **A native panel renders the feature's components, never a copy.** A genui `PanelProvider` fills data; its `Render` returns the SAME row/card/dialog the feature's page renders, parameterized only by the write target (`_target`), the slice, and instance ids. A chat-specific duplicate is a drift bug: the todo panel lost the shared badge, author, age, delete gate and native confirmation dialog, and only the page kept them. Reference: `features/todo/handlers/genui_panel.go` + `features/todo/components/panel.templ`.
- Don't abstract prematurely. Duplicate a little > create wrong abstraction.
- Don't hide dependencies behind globals/env checks deep in code - inject them.
- Keep seams discoverable (small files, clear names). If an LLM can't copy-paste and adapt it, it's too clever.

## SCOPE

Every non-test, non-generated `.go` under `internal/`/`features/` carries `// SCOPE:<core|plugin|feature> — <desc>` (`docs/scope-taxonomy.md`). The directory is the layer; the annotation records only the removal class. **`internal/` is Go's import boundary, not a removal class** — about half of it is plugin (`nats`, `collab`, `search`, `llm`, `datastar`, …), so read the annotation, never the directory. Enforced by `make check-scope` + pre-commit. Trim rule: never delete `core` without asking; `feature`/`plugin` delete freely per the inline description (`docs/features.md`).

## Hooks

Blocking gates live at git level via lefthook (`make setup`; wrappers committed in `.githooks/`). Pre-commit (parallel, glob-filtered): file-sizes · fmt-gofumpt · mod-tidy · govulncheck · ci-deps · install-sh · scope-lint · class-integrity · daisyui-lint · flatdeps-lint · test-value · docs-claims · secrets-env · rbac-policy (full: shape+coverage, `internal/rbac` + `internal/server`) · dagnats-pin · secrets-scan · skill-frontmatter · datastar-lint · api-routes · mcp-tests · registry-conformance · generated-templ · css-check · no-jsx · golangci-lint · agents-md-staleness · stelow-drift (advisory) · daisyui-drift (advisory) · docs-staleness (advisory). Pre-push (light): govulncheck · deadcode. Post-merge: regen-assets. Agent-level hooks keep only what git cannot (post-build hints). Details: `docs/local-ci.md` — T1 format/build → T2 scoped lint → T3 scoped tests → T3.5 `ci-local-fast` → T4 `ci-local` → T5 `signoff`, then push. Homolog light (no staging env): `make dev` + `make ci-local-fast` + smoke, share via `tailscale serve` — recipe in `docs/local-ci.md`.

## Gotchas (details in docs)

- **Routing:** compose in `internal/server/chi.go` — `BootChi` builds one handler per capability, `buildChiMux` flattens each capability's own router (auth included) onto the root with one `MountFeature` call (duplicate method+pattern = boot panic naming both owners; unknown method = 405; landing `/` is the not-found handler). Edge wrappers sit ABOVE the router (security headers → opt-in `HTTP_ACCESS_LOG` → same-origin guard → RBAC) so 403/404/405 all carry the same headers and log line; the guard rejects an unsafe cross-origin browser write even before RBAC, and `SameSite=Lax` remains the first CSRF layer; recovery is the only `r.Use`, and NO timeout or compression middleware may wrap SSE (a hidden `http.Flusher` or a write deadline kills the stream). Cookie is `gogogo_auth` (owned argon2id sessions, `internal/auth`); DagNats console on `:8090`. Static via `web/resources.AssetHandler` (`/static/` + root `/sw.js` with `Service-Worker-Allowed: /`).
- **RBAC (enforced by default in production; development starts open):** policy is route+verb (`internal/rbac/casbin/policy.csv`); per-record ownership is `canDeleteDoc`/`canDeleteRow` in each feature. A handler that reads or writes user data checks the session ITSELF (`sess == nil` → 401) — with RBAC off the policy decides nothing. An anonymous PAGE load the policy denies is bounced to `/login?next=…&auth=required` (the login page explains why); anonymous XHR/SSE gets 401, and a signed-in denial keeps 403. Operator surfaces do not rely on the flag: `/config` uses `rbac.RequireAdmin`, and `/console`/`/dagnats` require admin **and** an `ADMIN_EMAILS` match (empty list closes the console; a production demo admin is not an operator. A **development** demo with `DEMO_ACCOUNTS_ENABLED=true` adds `demo1@demo.app` to the allowlist so the console and stats menus are demonstrable). Adding a route means a `user` row + the `adminOnlyRoutes`/`publicRoutes` allowlist in `internal/rbac/coverage_test.go`; the coverage lint walks the real mux and demands a DIRECT `user` row (admin's `/*` masking a missing row is the bug that shipped). Objects are PATHS: `*` spans `/`, literals are escaped, metacharacters are refused at boot (`docs/rbac.md`).
- **Realtime:** mutations fan out twice — Hub record event to the owner's streams (no-echo on `clientID`, except offline replays which reach everyone) + JetStream CRUD op cross-instance — then re-list scoped. SSE hub (`/api/todos/stream`) is record-sync + ephemeral signals. Whiteboard/notes: SSEHub + NATS, Loro offline-first (`docs/async-layers.md`). Offline replay answers `"replayed"` + `X-Offline-Replay: 1` (and a receipt hit answers the same way); fan-out runs BEFORE that short-circuit, and a receipt hit does not re-publish — the first delivery already did.
- **Global/shell state: reuse, never poll.** One long-lived connection per page — five same-user tabs already fill Chromium's six-per-host budget, and a second stream per page hangs later POSTs (measured by the notes caret suite). Pages without a feature stream carry shell state on the leader session stream (`session.js`, Web Locks + BroadcastChannel); pages with one add the field to their own stream, or refresh on visibility/focus/online/pageshow (never on a timer). `data-on-interval`/`setInterval` that fetches fails `TestNoPeriodicFetch`; the reference is `internal/ui/inbox.templ` + `docs/realtime-recipe.md#global-state-and-the-connection-budget`.
- **Templ/CSS:** `make templ && make css` after `.templ` edits. `site/`+`docs/` are outside the Tailwind scan — landing edits can't stale the bundle (`docs/code-quality.md`).
- **Docs site engine:** `site/build.mjs`, `site/releases.mjs`, `site/search.mjs`, `site/search/search.js`, `site/search/minisearch.js`, `site/search/package.json`, `scripts/site-layout.test.mjs` and the two docsite scripts are vendored from `calionauta/docsite`, pinned in `site/UPSTREAM_SHA`. Never edit them here: fix upstream, then `scripts/bump-docsite.sh`. Project values (name, base, manifest, releases, search) live in `site/docsite.config.mjs`. The drift workflow fails on a local edit, and the layout test gates the pages publish. The landing is hand-written (`site/index.html`, `descent.css`, `official.css`); `node site/build-landing.mjs [--check]` regenerates/verifies its unit/gate catalog from `docs/unit-contracts.md` and `.lefthook.yml`.
- **Tests:** temp-dir Turso (`db.OpenAt` + `db.Migrate`) + real auth over `httptest`/real `:0` servers. Bind test servers to EPHEMERAL ports (`127.0.0.1:0`, NATS `-1`) and read the address back — a fixed port is stolen by another package under `-p N` (`address already in use`), which is what the old `-p 1` was hiding. Orphan `web` procs also hold `:18099`/`:4224` — `pkill -x web` (`docs/troubleshooting.md`).
- **Tests that spawn the real binary:** the flat binary opens no browser and takes no CLI subcommands — address + data dir ride env (`HOST`/`PORT`, `TURSO_DB_PATH`); keep `GOGOGO_NO_BROWSER=1` set explicitly in the child env anyway (cheap insurance if a launcher ever returns).
- **SSE in tests:** heartbeats flow on every open stream, so a parked `Read` must be deadline-bounded or the test hangs instead of failing — never assert on the raw transcript (`event:`/`data:` framing breaks `json.Unmarshal`); parse payloads. Ruleguard rule `BlockingReadBehindDeadline` guards the loop (`docs/local-ci.md`).
- **The service worker never touches `/api/*`.** It caches navigations only, so no stream, cached GET or mutation can be swallowed (`TestServiceWorkerNeverInterceptsAPI`). Offline writes belong to `web/resources/static/offline-outbox.js`: store the intent BEFORE the request, send its key as `Idempotency-Key`, and route a non-idempotent mutation (read-then-write) through `applyOnce` so the replay dedupes. A second queue — in the worker or a page — is the failure mode this design removed.
- **Tests other packages share:** never write a package global in a fixture — a write one test doesn't need is a latent race the moment two tests overlap. Set such state once in `TestMain`, or not at all.
- **Playwright in tests:** NEVER `waitUntil: "networkidle"` on a page that opens an SSE `EventSource` (whiteboard, notes) — the stream never lets the network go idle and `goto` burns its 30s timeout. Navigate with `waitUntil: "load"` + an explicit selector wait (`gotoBoard`/`gotoNote` in `scripts/`). Rule: if the page holds any long-lived connection, the readiness signal is a selector, never the network (`docs/local-ci.md`).
- **Git:** `git stash drop` is destructive (use `pop` or snapshot a `wip-*` branch first). Commit msgs via `git commit -F - <<'EOF'` (quoted EOF), never `git commit -m "$(cat <<EOF"`.
- **Deploy:** push-to-`master` ships in parallel with CI (deploy does NOT wait for it — PR CI is the pre-merge gate). CI builds the image once and the server **pulls it by digest** (`APP_IMAGE=…@sha256:…`); never a host-side binary, never a mutable tag. Container write via `setfacl`/`chmod`, NEVER `chown`. Never `scp` into the server clone. Healthcheck is `CMD ["/app","health"]` (`docs/deploy.md`).
- **UI and themes:** one component library, DaisyUI v5 (`/static/app.min.css`; NEVER `daisyui.min.css` v4 relic). Visual variants are DaisyUI themes picked from the sidebar; the catalog lives in `internal/themes` and must stay in step with `src/css/input.css` — MUST read `docs/ui-themes.md` (via `skills/gogogo-ui/SKILL.md`) before any `.templ`/CSS edit.
- **Config:** single source `config/config.go` (`docs/configuration.md`). Security defaults: `SIGNUP_ENABLED` and `DEMO_ACCOUNTS_ENABLED` are dev-on/production-off, `APP_URL` is required for out-of-band links (the request `Host` is never trusted), `REQUIRE_EMAIL_VERIFICATION`/`SESSION_IDLE_TIMEOUT` default off, and `RATE_LIMIT_RPM` bounds authenticated requests per session. Sign-out-everywhere uses the password when set, a current code when TOTP is enrolled, or the session when the account has no factor. TOTP disable and recovery rotation require the password or a current code on OAuth-only rows. Admin role/session mutations require the admin's own password when set, or a current code on a passwordless admin. The map is `docs/security.md`. Magic links are cross-device by default with a read-only GET + POST confirmation; `MAGIC_LINK_BIND_BROWSER=true` is strict. CSP/HSTS are app-owned (`internal/server/edge.go`); inline scripts carry the per-request nonce. **Docs:** `docs/*.md` is the source of truth — behaviour changes update the doc in the SAME commit; never edit `site/docs/` (generated, `make site`; `make site-check` green).

## Security invariants (agent gate)

These are the invariants a new route, handler or dependency must preserve. They
are enforced by tests/lints where the tool can see the shape; the rest is the
review checklist, and no LLM should self-certify it (see the testing rule).

- **Every route**: a session check in the handler (or `RequireSession`), a
  direct `user` row in `policy.csv`, and a line in `adminOnlyRoutes` or
  `publicRoutes`. Operator surfaces (`/config`, `/console`, `/dagnats`) keep
  their always-on fence independent of `RBAC_ENABLED`.
- **Every record read/write**: scope by the session owner in SQL
  (`GetXVisible` / `ListXForOwner`), not by a client-supplied ID. Deletes go
  through `canDeleteDoc`/`canDeleteRow`; a route-level policy is not ownership.
- **Every sink**: Templ escapes; `templ.Raw` only for a sanitizer-allowlisted
  fragment or a static map. No `Sprintf` HTML (ruleguard). SQL stays in sqlc
  with `?` parameters. File paths use `os.Root`, never a lexical prefix.
- **Every outbound fetch/command**: URLs and command names are operator
  config, never request input. A user-supplied URL needs a scheme/host
  allowlist and loopback/link-local denial; a shell is never used.
- **Every inline script**: `nonce={ templ.GetNonce(ctx) }` and the page carries
  `data-nonce` (CSP tests fail otherwise). No `unsafe-inline`/`unsafe-eval`
  for scripts.
- **Every password rotation, TOTP removal, and sign-out-everywhere**: revoke
  sessions and pending magic links; do not leave a 15-minute login window open.
  Sign-out-everywhere requires the password when the account has one; TOTP
  disable and recovery rotation require the password or a current code on
  OAuth-only rows. A failed proof is audited, not silently ignored.
- **Every admin mutation**: verify the signed-in admin's password or current
  authenticator/recovery code before role changes/session revocation; audit a
  failed proof and refuse the mutation.
- **Every audit event**: `security_events` carries event, reason, subject and
  actor only — never a password, token, TOTP code or magic-link token. Keep the
  vocabulary in `internal/auth/securitylog.go`; `/config` and the CSV read it.
- **Every request-scoped log**: `Error`/`Warn` in an HTTP slice goes through
  `internal/observability` with `WithComponent`; boot/worker logs may use
  `slog` because there is no request context. The audit table is not a log
  substitute.
- **Every new dependency**: `docs/dependency-principles.md`, pinned version,
  `govulncheck`; no native-floor change without a human gate.
- **Every security test**: prove it can fail (RED/GREEN/PROVE); a green test
  that cannot go red is not a guard.

## No AI attribution in commits or release notes

Claude or any AI assistant does NOT co-author anything here. Never add `Co-Authored-By: Claude` (or any model) or human trailers to commits, PRs, release notes, blog posts, CHANGELOG. Draft files (`/tmp/msg.txt`, `/tmp/notes.txt`) end at the last meaningful sentence.

## Releases (release-please owns versioning)

- Agents push feature/fix branches and open normal PRs ONLY. Never open a release PR, never `git tag`, never edit version numbers.
- **Pre-1.0, always, until a human asks otherwise.** A breaking change (`feat!:` or a `BREAKING CHANGE:` footer) bumps the MINOR inside 0.x — 0.54 → 0.55 — never to 1.0.0. 1.0.0 is a product statement (API stability, support window), not a side effect of a commit marker. The mapping is `bump-minor-pre-major: true` in `.github/release-please-config.json`, pinned by `internal/release_policy_test.go`: if a release PR says 1.0.0, the config is wrong — do not merge that PR.
- When you open the PR (`gh pr create`), fill EVERY `.github/PULL_REQUEST_TEMPLATE.md` section in the body: real linked issue (or delete the line), motivation/approach written by you, the one-line AI disclosure, verification with red→green evidence. No placeholders (`Closes #NNN`, `Write this yourself`) — the `pr-body` CI fails the PR on them. Never submit an empty body.
- The bot owns `CHANGELOG.md`. Never hand-edit it and never write an `[Unreleased]` narrative section: release-please GENERATES each version section and nothing reads `[Unreleased]`. Put the *why* in the PR body, where review reads it. Leave `changelog-type: github` alone — `default` parses raw commits and duplicates every merge commit whose body repeats the branch subject (`CHANGELOG.md` says why).
- After a master merge, tell the user a `chore: release X.Y.Z` PR will appear for THEM to review and merge (merge creates the tag; `release-please.yml` dispatches `release.yml`, which builds the installer binaries from it). Never merge it autonomously.
- Docs prose: one idea per paragraph, max three lines; 3+ items become a bold-lead list (`CONTRIBUTING.md`).

## Shared-host lint (server.calionauta.com)

- NEVER `golangci-lint run ./...` on this host — full-repo lint peaks near
  2.6 GB RSS and takes this 8 GB box (shared with bb + production) into swap.
  Use `make lint-safe` / `bash scripts/lint-safe.sh` (scopes to changed packages
  when RAM is tight, caps the run in a cgroup); otherwise `golangci-lint run <changed-pkgs>`.
- NEVER `golangci-lint cache clean` — the cache is what keeps a run cheap.
- NEVER full-suite `go test ./...` / `scripts/test-web.sh` / `make ci-local` on
  this host — same wedge, plus co-tenant agents share these cores (a second
  full run pushed this box to load 19). `make ci-local` refuses by itself via
  `check-host-capacity`; do not bypass with `CI_LOCAL_FORCE=1` here. Use
  scoped `go test <pkg>` or `make ci-local-fast` (carries its own
  `--fast` pre-flight + serial degrade past 8 changed packages) and let the
  PR's CI be the full gate.
- `deadcode` (pre-push) is **host-gated** and skips itself: it peaks at
  3.28 GB RSS, more than a full lint. A push once sat in that hook for 23
  minutes at load 18. If you see `deadcode SKIPPED`, that is the gate
  working — the push proceeds and CI runs the scan with real capacity. Do
  not add `DEADCODE_FORCE=1` here to "get the scan", and do not re-run a
  push just because it printed a skip.
- NEVER bare `go vet ./...` here, and think twice before
  `go test -run 'TestTrimmedTree...' ./internal/installer/` — each unit
  copies the whole repo and forks a full vet (parallel storm that took this
  box down). Those tests skip themselves on a saturated box via
  `internal/hostgate` (CI never skips); if you see the hostgate SKIP, run
  them on CI instead of forcing.
