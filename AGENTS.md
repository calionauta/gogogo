# gogogo

> Full-stack Go web app template — back-end + front-end + DB + auth + LLM + deploy in one binary.

## Project Overview

Go template: Datastar + Templ + Turso (sqlc) + owned auth + goqite + DagNats + NATS JetStream. Module: `github.com/calionauta/gogogo`

**Naming:** repo, module, binary, deploy dir (`/home/deploy/<APP_NAME>/`), container, tunnel hostname share the project name. Fresh clones carry `gogogo` in ~670 places across ~200 files — never rename by hand (run `python3 scripts/rename-project.py --dry-run my-app` for the current count). Use `go run ./cmd/gogogo` (guided; see `cmd/gogogo/README.md`) or `make rename NAME=my-app [OWNER=myorg]`.

**Upstream-first (generated projects keep this):** check the upstream template (`site/llms.txt` + `site/docs/<slug>/`, or the GitHub blob) BEFORE creating a feature or installing a library. Reuse what exists (Todo is the reference; `docs/unit-contracts.md` lists what each unit already owns). Scaffold a new feature with `go run ./cmd/gogogo new feature <name>` (wiring + registry + RBAC rows + proof) instead of copying a slice by hand. New dependency only when no upstream page covers the need.

**Unified build.** `go build ./cmd/web` compiles everything, no build tags. Opt out at runtime (`NATS_ENABLED=false`, `DAGNATS_ENABLED=false`, `GOAKT_ENABLED=false`). Backup: `scripts/backup.sh` (+ `restore.sh --dry-run` drill); retention per store: `docs/async-layers.md#durability-and-retention-per-store`.

## Stack (exact versions)

Go 1.27 | Templ v0.3.1020 | Datastar v1.0.4 (JS core, vendored) + datastar-go v1.2.2 (Go SDK) | Turso (tursogo v0.8.2, sqlc queries) | TailwindCSS v4.3.3 + DaisyUI v5.7.42 | goqite v0.4.0 | retry-go v4 | DagNats v0.0.30 | GoAkt v4.6.1 | NATS JetStream | age v1.3.2 | uuid v1.6.0

## Skills

- **Load them, don't paraphrase them.** Both skills are registered for agent hosts at `.agents/skills/<name>` (symlinks to `skills/<name>`), so a host can open them BY NAME — read the SKILL.md before the first `.go`/`.templ` edit of a session, not after a gate fails. `cmd/check-skill-frontmatter` (pre-commit + CI) fails when a skill is unregistered or a link dangles; nothing else enforces that they were read.
- `skills/gogogo-coding-standards` — Go + template rules (concurrency, perf, testing, Datastar, exception-to-go gate). Use when editing `.go`/`.templ`, spawning goroutines, wiring context, running lint/tests, profiling, or proposing native code. Universal principles live in the vendored `skills/stelow-workflow-coding-standards` (upstream [stelow](https://github.com/calionauta/stelow), pinned in its `UPSTREAM_SHA` — never edit the vendored copy in place). Drift: `bin/check-stelow-drift.sh` detects, `scripts/refresh-stelow.sh` fixes (a scheduled workflow opens the bump PR).
- `cali-code-navigation` — ripwire orient-first navigation. Use when landing cold in unfamiliar code or tracing callers.
- `/skill:cali-ops-deploy-github-tailscale` — server layout, deploy user, secret tables.
- **Agent tooling (install once per machine, index per repo — enforced by `make check-agent-tools`, runs in `make setup`):** `ripwire` (required, orient-first — install via `redhat-et/ripwire`, no daemon, no index step, call via bash); `cymbal` (required — `brew install cymbal`, then `cymbal index .` once per repo, auto-refresh after); `xerj` (required for docs/prose — `curl -fsSL https://xerj.org/get | sh`, then `xerj --insecure --data-dir ~/.xerj-data &` + `xerj autoindex .`, MCP via `xerj mcp`; keep its data dir OUTSIDE the repo, never commit indexes); `sem` (required — `brew install sem-cli`, no setup, cache outside the repo; if `sem --version` prints GNU parallel, put brew/cargo bin first in PATH).
- **Tool decision rule:** symbol/call-graph questions (who calls X, blast radius, tests to run, diff impact) → ripwire first (`ripwire . --for="<task>"`), cymbal to drill (`investigate`/`trace`/`impact`/`changed`). What changed / history / review evidence → `sem diff` (entities, not lines), `sem history` (blame, hotspots, co-change), `sem certify` (range certificate). Meaning/concept questions over code AND prose (docs audits, stale/contradiction sweeps, reference coding, cross-session memory) → xerj — cymbal/ripwire don't read prose.
- **Docs rule:** after ANY behavior change, sweep `docs/` + the site map (`site/build.mjs` MANIFEST) for staleness vs the diff — concept sweeps via xerj, exact-claim checks via grep/`sem` (verified: xerj lexical retrieval underperforms both on this corpus) — and confirm with `sem diff` (it diffs Markdown by heading — proves docs moved with code), update the doc in the SAME commit, verify with `make site-check` (docs-staleness hook stays advisory). **Never write a count by hand** (a hand-written capability count drifted from 24 to 35): counts live only in the generated `docs/unit-contracts.md#registry-at-a-glance`, and `make check-docs` rejects a hardcoded capability/slice/unit count, a dead repo path, an invented `gogogo` subcommand, and a docs page missing from the site MANIFEST. Changelog entries are release-please's job — it GENERATES them from conventional commits (never write `[Unreleased]` narrative; nothing promotes it).

## Commands

| Command | Description |
|---------|-------------|
| `make dev` | Air live reload |
| `make build` | Unified build, no lint/tests |
| `make templ` | Generate Templ |
| `make datastar-lint` | Lint `.templ` (`-only-errors` keeps intentional attrs) |
| `make css` / `make css-all` / `make css-check` | Rebuild one skin (DaisyUI) / every skin / verify all are current (scans `features/`, `web/`, `internal/`; commit `css-all`) |
| `make check-scope` | Assert `// SCOPE:<core|plugin|feature>` on every `internal/`+`features/` file |
| `make test` | Race tests, parallel across packages (CI runs them — prefer scoped tests locally) |
| `make check-generated` | Fail if committed `_templ.go` does not match the `.templ` sources |
| `make ci-local-fast` | Same cheap checks, but lint + tests scoped to changed packages |
| `make lint-safe` | Host-aware lint: sizes to free RAM/cores, scopes to changed pkgs when tight, memory-capped in a cgroup (`scripts/lint-safe.sh`) — prefer over `make lint` on a shared host |
| `make ci-local` | **Single gate** (= CI): checks + lint + build + boot smoke + parallel fan-out (race suite + browser smokes, one log each). If green, push. (`make check` was removed — redundant subset.) |
| `make signoff` | `ci-local` + advisory `gh signoff` stamp before push |
| `make setup` | Activate lefthook git hooks (`core.hooksPath=.githooks`) |
| `go run ./cmd/gogogo new feature <name>` | Scaffold one feature: handler + page, wiring, capability unit, RBAC rows, then gates + build proof (card in `docs/unit-contracts.md`) |

## Don'ts

- NO HTMX/Alpine (Datastar), NO `fmt.Sprintf` HTML (Templ), NO `log` (`log/slog`).
- Driver is `turso` (tursogo, registered by `internal/db`) — the only data layer; handlers call `dbgen.*` directly, no strategy switch. NO removing goqite for JetStream; different problems.
- NO hand-rolled IDs on rows — use `dbschema.NewID()` (UUIDv7 string, matches every owner column).
- NO authorization by ad-hoc `role == "admin"` in a handler (ruleguard `RoleCompareOutsideRBAC` fails CI) — route decisions go in `casbin/policy.csv`, per-record decisions in `canDeleteDoc`/`canDeleteRow`. NO shared-doc read/write without a session check in the handler: `RBAC_ENABLED` defaults to false, so the policy is inert until an operator opts in (`docs/rbac.md`).
- NO `PatchElements` without top-level `id` + `WithSelector` (`PatchElementsNoTargetsFound`). Use `internal/datastar.RenderAndPatch`.
- NO real LLM in tests — inject a stub (`internal/llm/fakeserver` only inside `internal/llm/`).
- NO secrets in git, chat-visible commands, or test fixtures — keys live in `~/.secrets/` (600) or age `.env.age` via `bin/init-secrets`; `gitleaks` pre-commit + CI enforce it. Never print secret values (lengths/names only).
- NO agent self-certification in-loop — tests guarding new behavior follow RED/GREEN/PROVE (acceptance from human/fresh context → implement → hand-mutate, watch fail, revert; never commit the mutation); no mandated TDD red-green cycle (evidence: ProgramBench + DeepSWE 2026). Normative text: `skills/gogogo-coding-standards/references/go-testing.md`.
- Prefer Datastar attributes over vanilla JS; inline JS only adjacent to the markup.
- NO `go install <tool>@latest` for the gates — external tools are pinned by the `tool` directive in `go.mod` (already the case for `templ`) and run as `go tool <name>`. An unpinned tool can be older than the Go toolchain, which is how `deadcode` once reported type-check errors as dead code. Add one with `go get -tool <pkg>@<version>`.
- NO `make check`, NO whole-repo `golangci-lint run ./...` for small changes (scope to touched pkgs — or just `make lint-safe`, which sizes the run to the host's free RAM/cores and caps it in a cgroup), NO `golangci-lint cache clean` (the cache is what keeps a run cheap; a cold run re-type-checks the whole module and spikes memory — it has taken a shared host into swap), NO `go build -tags "<stale>"` (unified-build era has no tags). Project-specific footguns go in `rules/rules.go` (ruleguard, loaded by gocritic) — a rule there fails CI, so prose guidance that a linter can enforce belongs there, not in this file.
- NO `go func()` loop without a shutdown path: long-lived loops select on `ctx.Done()` (or a `done chan struct{}`), bound at wiring time. NO `ctx` stored in a struct. NO bare `for range ticker.C`. See `skills/gogogo-coding-standards` (Concurrency) + [docs/code-quality.md](docs/code-quality.md#concurrency-and-resources).
- NO `--no-verify` commits to dodge a red gate — a red hook is a finding about your change, not an obstacle. Fix it, narrow the scope, or drop the change. (Precedent: a funlen breakage shipped past local hooks this way and failed CI instead.)
- NO legacy until 1.0 (only the human declares 1.0, never an LLM): no backward-compat code, no fallback branches for removed shapes, no deprecated aliases, no `legacy*` helpers, no comments explaining current behavior by reference to removed code — delete the old path, do not bridge it. Reword history-leaning comments to present tense. Exceptions (the only ones): DB migrations stay append-only (data loss beats code purity), and decision records (`docs/architecture-decision.md`, CHANGELOG) keep history — recording decisions is their job.
- After manual smoke/debug runs: `make smoke-clean` — binaries and tmp dirs survive failures and interrupts (the suite only removes them on the happy path). Test artifacts live in the OS temp dir by construction (`SMOKE_BIN`, never `/tmp`-hardcoded); never leave binaries in the repo (a 44MB binary was once committed by accident). Never delete Playwright browsers in cleanup (per-OS shared cache, GBs to re-download).

## Go-first / Zig (summary; normative: `docs/exception-to-go.md`)

Go is the product language (~95-99%). Zero Zig code/toolchain in tree — do not add without a benchmarked case. Bans: "Zig is faster", manual memory, "low-level", "could be optimized", "looks like SIMD", preference, avoiding a Go dep, future perf. Go SIMD (`simd`/`archsimd`, `GOEXPERIMENT=simd`) first; perf work needs a Go baseline bench + profile proving Go is the bottleneck. Kernel rules: one package, small C ABI, caller-owned buffers, pure-Go fallback day one, removable in minutes. Vague "use Zig when appropriate" is rejected — follow the doc's decision procedure. **A native UI in Zig = [`nanoui`](https://github.com/justrach/nanoui)** (immediate-mode, Metal/CPU-rasterizer, ~0.7 MB, MIT) as a SEPARATE binary like `cmd/desktop` — never a Zig kernel inside `cmd/web`; pin its release tag.

## Architecture seams

Keep architecture explicit, thin, and easy for LLMs to evolve. Avoid creating a framework.

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
- Don't abstract prematurely. Duplicate a little > create wrong abstraction.
- Don't hide dependencies behind globals/env checks deep in code - inject them.
- Keep seams discoverable (small files, clear names). If an LLM can't copy-paste and adapt it, it's too clever.

## SCOPE

Every non-test, non-generated `.go` under `internal/`/`features/` carries `// SCOPE:<core|plugin|feature> — <desc>` (`docs/scope-taxonomy.md`). The directory is the layer; the annotation records only the removal class. **`internal/` is Go's import boundary, not a removal class** — about half of it is plugin (`nats`, `collab`, `search`, `llm`, `datastar`, …), so read the annotation, never the directory. Enforced by `make check-scope` + pre-commit. Trim rule: never delete `core` without asking; `feature`/`plugin` delete freely per the inline description (`docs/features.md`).

## Hooks

Blocking gates live at git level via lefthook (`make setup`; wrappers committed in `.githooks/`). Pre-commit (parallel, glob-filtered): file-sizes · fmt-gofumpt · mod-tidy · install-sh · scope-lint · flatdeps-lint · docs-claims · secrets-env · secrets-scan · rbac-policy (full: shape+coverage, `internal/rbac` + `internal/server`) · dagnats-pin · skill-frontmatter · datastar-lint · api-routes · registry-conformance · generated-templ · css-check · golangci-lint · agents-md-staleness · docs-staleness (advisory) · stelow-drift (advisory) · govulncheck/ci-deps (glob-scoped). Pre-push (light): govulncheck · deadcode. Post-merge: regen-assets. Agent-level hooks keep only what git cannot (post-build hints). Details: `docs/local-ci.md` — T1 format/build → T2 scoped lint → T3 scoped tests → T3.5 `ci-local-fast` → T4 `ci-local` → T5 `signoff`, then push. Homolog light (no staging env): `make dev` + `make ci-local-fast` + smoke, share via `tailscale serve` — recipe in `docs/local-ci.md`.

## Gotchas (details in docs)

- **Routing:** compose in `internal/server/chi.go` — `BootChi` builds one handler per capability, `buildChiMux` flattens each capability's own router (auth included) onto the root with one `MountFeature` call (duplicate method+pattern = boot panic naming both owners; unknown method = 405; landing `/` is the not-found handler). Edge wrappers sit ABOVE the router (security headers → opt-in `HTTP_ACCESS_LOG` → same-origin guard → RBAC) so 403/404/405 all carry the same headers and log line; the guard rejects an unsafe cross-origin browser write even before RBAC, and `SameSite=Lax` remains the first CSRF layer; recovery is the only `r.Use`, and NO timeout or compression middleware may wrap SSE (a hidden `http.Flusher` or a write deadline kills the stream). Cookie is `gogogo_auth` (owned argon2id sessions, `internal/auth`); DagNats console on `:8090`. Static via `web/resources.AssetHandler` (`/static/` + root `/sw.js` with `Service-Worker-Allowed: /`).
- **RBAC (opt-in, `RBAC_ENABLED=false` by default):** policy is route+verb (`internal/rbac/casbin/policy.csv`); per-record ownership is `canDeleteDoc`/`canDeleteRow` in each feature. A handler that reads or writes user data checks the session ITSELF (`sess == nil` → 401) — with RBAC off the policy decides nothing. Adding a route means a `user` row + the `adminOnlyRoutes`/`publicRoutes` allowlist in `internal/rbac/coverage_test.go`; the coverage lint walks the real mux and demands a DIRECT `user` row (admin's `/*` masking a missing row is the bug that shipped). Objects are PATHS: `*` spans `/`, literals are escaped, metacharacters are refused at boot (`docs/rbac.md`).
- **Realtime:** mutations fan out twice — Hub record event to the owner's streams (no-echo on `clientID`, except offline replays which reach everyone) + JetStream CRUD op cross-instance — then re-list scoped. SSE hub (`/api/todos/stream`) is record-sync + ephemeral signals. Whiteboard/notes: SSEHub + NATS, Loro offline-first (`docs/async-layers.md`). Offline replay answers `"replayed"` + `X-Offline-Replay: 1`; fan-out runs BEFORE that short-circuit.
- **Templ/CSS:** `make templ && make css-all` after `.templ` edits. `site/`+`docs/` are outside the Tailwind scan — landing edits can't stale the bundle (`docs/code-quality.md`).
- **Docs site engine:** `site/build.mjs`, `site/releases.mjs`, `site/search.mjs`, `site/search/search.js`, `site/search/minisearch.js`, `site/search/package.json`, `scripts/site-layout.test.mjs` and the two docsite scripts are vendored from `calionauta/docsite`, pinned in `site/UPSTREAM_SHA`. Never edit them here: fix upstream, then `scripts/bump-docsite.sh`. Project values (name, base, manifest, releases, search) live in `site/docsite.config.mjs`. The drift workflow fails on a local edit, and the layout test gates the pages publish.
- **Tests:** temp-dir Turso (`db.OpenAt` + `db.Migrate`) + real auth over `httptest`/real `:0` servers. Bind test servers to EPHEMERAL ports (`127.0.0.1:0`, NATS `-1`) and read the address back — a fixed port is stolen by another package under `-p N` (`address already in use`), which is what the old `-p 1` was hiding. Orphan `web` procs also hold `:18099`/`:4224` — `pkill -x web` (`docs/troubleshooting.md`).
- **Tests that spawn the real binary:** the flat binary opens no browser and takes no CLI subcommands — address + data dir ride env (`HOST`/`PORT`, `TURSO_DB_PATH`); keep `GOGOGO_NO_BROWSER=1` set explicitly in the child env anyway (cheap insurance if a launcher ever returns).
- **SSE in tests:** heartbeats flow on every open stream, so a parked `Read` must be deadline-bounded or the test hangs instead of failing — never assert on the raw transcript (`event:`/`data:` framing breaks `json.Unmarshal`); parse payloads. Ruleguard rule `BlockingReadBehindDeadline` guards the loop (`docs/local-ci.md`).
- **Tests other packages share:** never write a package global in a fixture — a write one test doesn't need is a latent race the moment two tests overlap. Set such state once in `TestMain`, or not at all.
- **Playwright in tests:** NEVER `waitUntil: "networkidle"` on a page that opens an SSE `EventSource` (whiteboard, notes) — the stream never lets the network go idle and `goto` burns its 30s timeout. Navigate with `waitUntil: "load"` + an explicit selector wait (`gotoBoard`/`gotoNote` in `scripts/`). Rule: if the page holds any long-lived connection, the readiness signal is a selector, never the network (`docs/local-ci.md`).
- **Git:** `git stash drop` is destructive (use `pop` or snapshot a `wip-*` branch first). Commit msgs via `git commit -F - <<'EOF'` (quoted EOF), never `git commit -m "$(cat <<EOF"`.
- **Deploy:** push-to-`master` ships in parallel with CI (deploy does NOT wait for it — PR CI is the pre-merge gate). Container write via `setfacl`/`chmod`, NEVER `chown`. Never `scp` into the server clone (`git pull --ff-only`). Scratch healthcheck: `CMD ["/app","health"]` (`docs/deploy.md`).
- **Skins:** default DaisyUI v5 (`/static/app.min.css`; NEVER `daisyui.min.css` v4 relic). Basecoat and shadcn have their own vocabularies — read `docs/ui-skins.md` first.
- **Config:** single source `config/config.go` (`docs/configuration.md`). **Docs:** `docs/*.md` is the source of truth — behaviour changes update the doc in the SAME commit; never edit `site/docs/` (generated, `make site`; `make site-check` green).

## No AI attribution in commits or release notes

Claude or any AI assistant does NOT co-author anything here. Never add `Co-Authored-By: Claude` (or any model) or human trailers to commits, PRs, release notes, blog posts, CHANGELOG. Draft files (`/tmp/msg.txt`, `/tmp/notes.txt`) end at the last meaningful sentence.

## Releases (release-please owns versioning)

- Agents push feature/fix branches and open normal PRs ONLY. Never open a release PR, never `git tag`, never edit version numbers.
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
