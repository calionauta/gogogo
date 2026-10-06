# gogogo

> Full-stack Go web app template — back-end + front-end + DB + auth + LLM + deploy in one binary.

## Project Overview

Go template: Datastar + Templ + PocketBase + goqite + DagNats + NATS JetStream. Module: `github.com/calionauta/gogogo`

**Naming:** repo, module, binary, deploy dir (`/home/deploy/<APP_NAME>/`), container, tunnel hostname share the project name. Fresh clones carry `gogogo` in ~360 places across ~130 files — never rename by hand. Use `go run ./cmd/gogogo` (guided; see `cmd/gogogo/README.md`) or `make rename NAME=my-app [OWNER=myorg]`.

**Upstream-first (generated projects keep this):** check the upstream template (`site/llms.txt` + `site/docs/<slug>/`, or the GitHub blob) BEFORE creating a feature or installing a library. Reuse what exists (Todo is the reference; SCOPE says what is safe to delete). New dependency only when no upstream page covers the need.

**Unified build.** `go build ./cmd/web` compiles everything, no build tags. Opt out at runtime (`NATS_ENABLED=false`, `DAGNATS_ENABLED=false`).

## Stack (exact versions)

Go 1.27 | Templ v0.3.1020 | Datastar v1.2.2 | PocketBase v0.40.4 (ncruces/go-sqlite3) | TailwindCSS v4.3.3 + DaisyUI v5.7.42 | goqite v0.4.0 | retry-go v4 | DagNats v0.0.24 | NATS JetStream | age v1.3.2 | uuid v1.6.0

## Skills

- `skills/gogogo-coding-standards` — Go + template rules (concurrency, perf, testing, Datastar, Zig gate). Use when editing `.go`/`.templ`, spawning goroutines, wiring context, running lint/tests, profiling, or proposing native code. Universal principles delegated to [`stelow-workflow-coding-standards`](https://github.com/calionauta/stelow/tree/main/skills/stelow-workflow-coding-standards).
- `cali-code-navigation` — ripwire orient-first navigation. Use when landing cold in unfamiliar code or tracing callers.
- `/skill:cali-ops-deploy-github-tailscale` — server layout, deploy user, secret tables.

## Commands

| Command | Description |
|---------|-------------|
| `make dev` | Air live reload |
| `make build` | Unified build, no lint/tests |
| `make templ` | Generate Templ |
| `make datastar-lint` | Lint `.templ` (`-only-errors` keeps intentional attrs) |
| `make css` / `make css-check` | Rebuild / verify Tailwind bundle (scans `features/`, `web/`, `internal/`) |
| `make check-scope` | Assert `// SCOPE:layer=…,removal=…` on every `internal/`+`features/` file |
| `make test` | Race tests, parallel across packages (CI runs them — prefer scoped tests locally) |
| `make ci-local` | **Single gate** (= CI): templ + datastar-lint + css-check + check-scope + lint + race tests + build. If green, push. (`make check` was removed — redundant subset.) |
| `make signoff` | `ci-local` + advisory `gh signoff` stamp before push |
| `make setup` | Activate lefthook git hooks (`core.hooksPath=.githooks`) |

## Don'ts

- NO HTMX/Alpine (Datastar), NO `fmt.Sprintf` HTML (Templ), NO `log` (`log/slog`).
- Driver is `ncruces/go-sqlite3` (registers `sqlite3`) — never switch to PocketBase's bundled modernc (`sqlite`, unused). NO removing goqite for JetStream; different problems.
- NO manual `id` on PocketBase records (PK max 15, `^[a-z0-9]+$`).
- NO `PatchElements` without top-level `id` + `WithSelector` (`PatchElementsNoTargetsFound`). Use `internal/datastar.RenderAndPatch`.
- NO real LLM in tests — inject a stub (`internal/llm/fakeserver` only inside `internal/llm/`).
- Prefer Datastar attributes over vanilla JS; inline JS only adjacent to the markup.
- NO `make check`, NO whole-repo `golangci-lint run ./...` for small changes (scope to touched pkgs), NO `go build -tags "<stale>"` (unified-build era has no tags). Project-specific footguns go in `rules/rules.go` (ruleguard, loaded by gocritic) — a rule there fails CI, so prose guidance that a linter can enforce belongs there, not in this file.
- NO `go func()` loop without a shutdown path: long-lived loops select on `ctx.Done()` (or a `done chan struct{}`), bound at wiring time. NO `ctx` stored in a struct. NO bare `for range ticker.C`. See `skills/gogogo-coding-standards` (Concurrency) + [docs/code-quality.md](docs/code-quality.md#concurrency-and-resources).

## Go-first / Zig (summary; normative: `docs/native-zig.md`)

Go is the product language (~95-99%). Zero Zig code/toolchain in tree — do not add without a benchmarked case. Bans: "Zig is faster", manual memory, "low-level", "could be optimized", "looks like SIMD", preference, avoiding a Go dep, future perf. Go SIMD (`simd`/`archsimd`, `GOEXPERIMENT=simd`) first; perf work needs a Go baseline bench + profile proving Go is the bottleneck. Kernel rules: one package, small C ABI, caller-owned buffers, pure-Go fallback day one, removable in minutes. Vague "use Zig when appropriate" is rejected — follow the doc's decision procedure.

## SCOPE

Every non-test, non-generated `.go` under `internal/`/`features/` carries `// SCOPE:layer=<infra|feature>,removal=<core|plugin|feature>` (`docs/scope-taxonomy.md`). Enforced by `make check-scope` + pre-commit. Trim rule: never delete `removal=core` without asking; `feature`/`plugin` delete freely per the inline description (`docs/features.md`).

## Hooks

Blocking gates live at git level via lefthook (`make setup`; wrappers committed in `.githooks/`). Pre-commit (parallel, glob-filtered): file-sizes · fmt · mod-tidy · scope-lint · datastar-lint · css-check · golangci-lint · agents-md-staleness · docs-staleness (advisory). Pre-push (light): govulncheck · deadcode. Agent-level hooks keep only what git cannot (post-build hints). Details: `docs/local-ci.md` — T1 format/build → T2 scoped lint → T3 scoped tests → T4 `ci-local` → T5 `signoff`, then push.

## Gotchas (details in docs)

- **Routing:** register DIRECTLY on `se.Router` inside OnServe (nested `OnServe().BindFunc` never fires; `GET /` swallows subpaths). Cookie is `gogogo_auth`, NOT `pb_auth` (PB admin/users are separate namespaces — sharing clobbers the admin session); admin UI on separate origin `:8090/_/`. Static assets via EXACT `/static/<file>` routes with content-hash ETag (`ARCHITECTURE.md`, `docs/architecture.md`).
- **Realtime:** todo mutations flow through PB realtime (`/api/realtime`, per-user rules) + fragment re-fetch — do NOT add a parallel SSE-hub re-render. SSE hub (`/api/todos/stream`) is ephemeral signals only. Whiteboard: SSEHub + NATS, Loro offline-first (`docs/async-layers.md`).
- **Templ/CSS:** `make templ && make css` after `.templ` edits. `site/`+`docs/` are outside the Tailwind scan — landing edits can't stale the bundle (`docs/code-quality.md`).
- **Tests:** temp-dir PB + `Bootstrap()` + real SQLite over `httptest`. Bind test servers to EPHEMERAL ports (`127.0.0.1:0`, NATS `-1`) and read the address back — a fixed port is stolen by another package under `-p N` (`address already in use`), which is what the old `-p 1` was hiding. Orphan `web` procs also hold `:18099`/`:4224` — `pkill -x web` (`docs/troubleshooting.md`).
- **Tests that spawn the real binary:** set `GOGOGO_NO_BROWSER=1` explicitly in the child env, built from scratch (not `append(os.Environ(), …)` — an inherited `=0` wins and a browser tab opens). The child inherits the parent's TTY, so `interactive()` detection alone lets `make test` from a terminal through; PB's first-run installer then calls `LaunchURL` (`docs/troubleshooting.md`).
- **SSE in tests:** `pumpSSEUntil` must enforce its deadline while a `Read` is parked — a `for time.Now().Before(deadline) { Body.Read }` loop waits one heartbeat (15s) past it. Predicates parse payloads via `sseSignalPayloads`, never the raw transcript (it is `event:`/`data:` lines and each payload carries a `signals ` prefix, so `json.Unmarshal` always fails and the predicate never fires). A negative assertion uses `pumpSSEFor` + `sseAbsenceWindow`. Ruleguard rule `BlockingReadBehindDeadline` guards the loop (`docs/local-ci.md`).
- **Tests other packages share:** never write a package global in a fixture. `auth.CookieSecure` was assigned `false` (its zero value) by four fixtures — a latent race the moment two tests in a package overlap. Set such state once in `TestMain`, or not at all.
- **Git:** `git stash drop` is destructive (use `pop` or snapshot a `wip-*` branch first). Commit msgs via `git commit -F - <<'EOF'` (quoted EOF), never `git commit -m "$(cat <<EOF"`.
- **Deploy:** push-to-`master` → CI gate → deploy. Container write via `setfacl`/`chmod`, NEVER `chown`. Never `scp` into the server clone (`git pull --ff-only`). Scratch healthcheck: `CMD ["/app","health"]` (`docs/deploy.md`).
- **Skins:** default DaisyUI v5 (`/static/app.min.css`; NEVER `daisyui.min.css` v4 relic). Basecoat/Morpheus have their own vocab — read `docs/ui-skins.md` first.
- **Config:** single source `config/config.go` (`docs/configuration.md`). **Docs:** `docs/*.md` is the source of truth — behaviour changes update the doc in the SAME commit; never edit `site/docs/` (generated, `make site`; `make site-check` green).

## No AI attribution in commits or release notes

Claude or any AI assistant does NOT co-author anything here. Never add `Co-Authored-By: Claude` (or any model) or human trailers to commits, PRs, release notes, blog posts, CHANGELOG. Draft files (`/tmp/msg.txt`, `/tmp/notes.txt`) end at the last meaningful sentence.
