# gogogo

> Agent-native product foundation in Go — back-end + front-end + DB + auth + LLM + deploy in one binary.

## Project Overview

Datastar + Templ + Turso (sqlc) + owned auth + goqite + DagNats + NATS JetStream. Module `github.com/calionauta/gogogo`.
**Naming:** repo/binary/deploy-dir/container/hostname share the project name; `gogogo` appears ~670× — never rename by hand (`scripts/rename-project.py --dry-run`, `make rename`). Scaffold via `go run ./cmd/gogogo` or `make rename`.
**Scaffold flags:** `--shape full|web|api|desktop|gui|mobile` prunes entrypoints (`docs/shape.md`; never changes what a page shows), `--recipes none|all|<ids>` drops demo slices (`none` is the default, gated on real imports — a slice a kept file still imports stays and the plan names the importer). Agents: `--no-tui --yes`, preview with `--dry-run --format json`.
**Agent index:** start at `llms.txt` (curated entry index: start-here, by-task, invariants, plus a generated "every published page" block). `site/docs/<slug>/` renders it, and `site/docs/<slug>.md` is a per-page markdown copy an agent can fetch without the whole site.
**No checkout needed:** `go install github.com/calionauta/gogogo/cmd/gogogo@latest` (or `.../cmd/gogogo-mcp@latest`) gives the whole evolution surface; `llms.txt` names the order — `advise --need …` → `recipe source <id>` → `recipe install <id>` only when the copy must be owned.
**Upstream-first:** check `llms.txt` + `site/docs/<slug>/` before adding a feature/dependency; Todo is the reference, `docs/unit-contracts.md` lists owners. Read a slice before writing one: `gogogo recipe source <id>` (no receipt, no copy — installing is a separate, deliberate act). New features: `go run ./cmd/gogogo new feature <name>`. Languages: `gogogo locales` (`locales.yml` is the single record every gate reads). Metric labels translate in the declaration's `translations:` block; analytics is a sink on the event bus (`docs/hooks-analytics.md`).
**Build:** `go build ./cmd/web`, no tags; opt out at runtime (`NATS_ENABLED=false` etc.). Backup `scripts/backup.sh`; retention `docs/async-layers.md`.

## Stack

Go 1.27 | Templ v0.3.1020 | Datastar v1.0.4 + datastar-go v1.2.2 | Turso (tursogo v0.8.3) | Tailwind v4.3.3 + DaisyUI v5.7.42 | goqite v0.4.0 | DagNats v0.0.30 | GoAkt v4.6.3 | NATS JetStream | age v1.3.2 | i18n seam (`internal/i18n`, default off).

## Skills

Read the SKILL.md by name before the first matching edit (symlinks in `.agents/skills/`); `cmd/check-skill-frontmatter` fails on unregistered links. Load via `/skill:gogogo-coding-standards`, `/skill:gogogo-ui`, `/skill:gogogo-recipe-authoring`.
- `gogogo-coding-standards` — Go/template gates (concurrency, Datastar, perf, exception-to-go). On `.templ` it fires with `gogogo-ui`. Universal rules: vendored `stelow-workflow-coding-standards` (pinned `UPSTREAM_SHA`, never edit; `scripts/refresh-stelow.sh`).
- `gogogo-ui` — DaisyUI vocabulary, semantic colors, `app.min.css`. Read with `docs/ui-themes.md` before any `.templ`/CSS edit. Vocabulary: vendored `daisyui` (pinned; `scripts/refresh-daisyui.sh`).
- `gogogo-adopt` — whether gogogo fits at all, and the ask-first route (advise → plan → **ask the operator for name/owner/dir/shape/locales** → apply). A static or landing page is not a unit here (`GET /` redirects to `/login`). Read before proposing or running a scaffold.
- `gogogo-recipe-authoring` — recipe contract (owns/must-not-touch, slots, receipts, declared metrics/outcomes, upgrade path). Read before creating/editing a recipe.
- `cali-code-navigation` — ripwire orient-first; `cali-ops-deploy-github-tailscale` — server/secrets.
- **Tools (enforced by `make check-agent-tools`):** `ripwire` (orient), `cymbal` (`cymbal index .`), `xerj` (docs/prose, data outside repo), `sem` (diff/history). Rule: symbol/call-graph → ripwire/cymbal; history/diff → `sem`; meaning over code+prose → xerj.
- **Docs:** behavior changes update the page that describes them in the SAME commit — `docs/` for prose, `llms.txt` for the agent index, `skills/` for the authoring contract, `site/index.html` for the landing. `docs/glossary.md` is where a term is DEFINED (one meaning per term; a new term is defined there in the same commit; the retired names are rejected by `make check-docs`). Counts live only in generated `docs/unit-contracts.md`; `make check-docs` rejects hand counts, dead paths, invented subcommands, undocumented CLI verbs, a stale generated MCP table, and a feedback tier that one page names and another does not (`AGENTS.md` and `docs/local-ci.md` describe the same ladder). The pre-commit staleness hook names the page to review; the PR body names the page you touched. Research/evaluations → `docs/research/` (`Reviewed:` + hub row + MANIFEST + `make docs-index`). A page in `docs/` is published or declared (`unpublishedDirs`/`unpublishedFiles` in `site/docsite.config.mjs`) — the site build fails otherwise, and `make check-docs` fails when a published page is missing from the generated block in `llms.txt`.

## Surfaces

- **Evolution** = `cmd/gogogo` + `cmd/gogogo-mcp` (stdio). Developer only; may rewrite the checkout. Never ship this MCP to a product's users.
- **Consumer** = `cmd/product-mcp` (stdio) + `cmd/web` routes + `POST /api/ext/invoke` + `GET /api/ext/descriptors`, fenced by session/RBAC. Every shape ships `cmd/product-mcp`, so a product's users and their agents drive it without the evolution toolchain installed. Both clients share `internal/productapi`; `cmd/check-product-boundary` fails if `cmd/product-mcp` is missing or if either half imports the other. Loopback v1; remote via SSH/Tailscale, never public. No API-token system until a concrete consumer + threat model (`docs/mcp.md`).

## Commands

| Command | Use |
|---|---|
| `make dev` / `make build` | Air reload / unified build |
| `make desktop`\|`gui`\|`mobile`\|`mygo` | Native targets, local-only |
| `make templ` / `make datastar-lint` / `make css` / `make css-check` | Templ gen / lint / bundle / verify |
| `make test` | Race tests (prefer scoped locally) |
| `make check-generated` / `check-ci-scripts` | Templ-drift gate / CI-script contracts |
| `make check-shape` / `check-domains` | App-shape record / one card per feature slice |
| `make ci-local-fast` | Cheap gate, scoped to changed pkgs |
| `make verify-local` / `make verify-local-share` | Boot the real binary on a fresh data dir: URL + account + the routes the diff touched — the operator's 60 s click, after `ci-local-fast`. Records PASS/FAIL as the stamp `make signoff` requires |
| `make test-recipe R=<id>` | Run the tests ONE recipe owns (package scope derived from its receipt) |
| `make lint-safe` | Host-aware lint (over whole-repo lint on shared host) |
| `make ci-local` | Single gate (= CI). If green, push. |
| `make signoff` | `check-operator-look` + `ci-local` + stamp before push — refuses a page/CSS diff with no recorded operator PASS |
| `make setup` / `make go-bump` | Hooks (`core.hooksPath=.githooks`) / Go patch bump (minor is human-only) |
| `go run ./cmd/gogogo new feature <name>` | Scaffold + gates + build proof |
| `go run ./cmd/gogogo recipe source <id>` / `recipe receipts --write` | Read a slice without installing it / re-hash a recipe file you edited |
| `go run ./cmd/gogogo --shape … --recipes …` | Scaffold a product (flags in Project Overview) |

## Don'ts

- NO HTMX/Alpine, NO `fmt.Sprintf` HTML, NO `log` (`slog`).
- NO prompt text in Go. A model-facing instruction is a file in `internal/prompts/files/<domain>/<name>.prompt` (Dotprompt: YAML frontmatter + `{{variable}}`), loaded via `internal/prompts`; `make check-prompts` fails on a prompt-shaped literal. See `docs/prompts.md`.
- NO hand-rolled IDs (`dbschema.NewID()`), NO ad-hoc `role == "admin"` (Casbin + `canDelete*` only), NO `PatchElements` without `id` + `WithSelector` (use `internal/datastar.RenderAndPatch`).
- NO second i18n library: strings via `internal/i18n.T(ctx, "dominio.chave", nil)`. NO hardcoded copy in new markup.
- NO real LLM in tests (stub only), NO secrets in git/chat/fixtures (`~/.secrets/` 600 or age `.env.age`; never print values).
- NO agent self-certification: RED/GREEN/PROVE — hand-mutate, watch fail, revert; never commit the mutation.
- NO new dep without `docs/dependency-principles.md` + pinned version + PR trade-off line; native-floor changes are human gates.
- NO `go install <tool>@latest` for gates (`go tool <name>`, pinned in `go.mod`). NO `make check`, NO `golangci-lint run ./...` (use `make lint-safe`), NO `cache clean`, NO `go build -tags`.
- NO `go func()` without shutdown (`ctx.Done()`), NO `ctx` in struct, NO `for range ticker.C`.
- NO `--no-verify` to dodge red gates. NO legacy until 1.0 (delete old path; migrations append-only excepted).
- After smoke runs: `make smoke-clean`. Never delete Playwright browsers.

## Go-first / Zig

Normative: `docs/exception-to-go.md`. Go ~95-99%, zero Zig in tree. Bans: "Zig is faster", manual memory, preference. Go SIMD first; perf needs Go baseline + profile. Native UI = `nanoui` as a separate binary, never a kernel in `cmd/web`.

## Architecture seams

- Thin seams, direct calls, explicit DI: small 1-3-method interfaces only when 2+ impls exist; no containers, no pass-throughs, no `GenericRepo[T]`. Fail fast.
- **Driver by target:** tursogo (`turso`) web/desktop, ncruces (`sqlite3`) gui/mobile; one schema. All driver *loading* — cache dir, cross-process lock, hash-mismatch repair — is `internal/dbload` (`Ensure`/`PrepareWorkspace`); nothing else names a driver. Swapping engines = the DSN file + one build-tagged file (`docs/stack-layers.md#swapping-the-driver`).
- **DATA_DIR is the root:** every store path (`TURSO_DB_PATH`, `NATS_STORE_DIR`, `DAGNATS_STORE_DIR`, `LICENSE_FILE`, queues) defaults *under* it. A new store that hardcodes `data/…` is invisible to a backup of `DATA_DIR`. An explicit env var still wins.
- **Service layer:** `features/<name>/service/` owns writes (`List/Create/Delete`, `canDeleteRow`); handlers/native shells call it. Vertical slices first; duplicate > wrong abstraction.
- **A slice's written boundary** is `docs/domain/<feature>.md` — owns / does-not-own / language / invariants, gated by `make check-domains`. Read it before editing the slice; update it in the same commit as a behaviour change.
- **Native panels render the page's components, never a copy** (`genui_panel.go` reuses `features/todo/components/panel.templ` pattern or it's drift).
- Removal class is `Capability.Kind` only (`docs/scope-taxonomy.md`); `internal/` is the import boundary, not the class.

## Hooks

lefthook pre-commit (fmt, `govulncheck`, `datastar-lint`, `css-check`, `api-routes`, `registry-conformance`, skill-frontmatter, advisory drifts); pre-push `govulncheck`+`deadcode` (host-gated, may skip) + `operator-look` (refuses a page/CSS diff with no recorded PASS). Details `docs/local-ci.md`: T1 format → T2 lint → T3 tests → T4 `ci-local-fast` → T5 `verify-local` (the operator's click) → T6 `ci-local` → T7 `signoff`.

## Gotchas

Routing: one `MountFeature` per capability in `chi.go`; edge wrappers above router (headers → guard → RBAC); no timeout/compression on SSE; cookie `gogogo_auth`. RBAC on in prod, open in dev — handlers always self-check session (`docs/rbac.md`). Realtime: Hub + JetStream fan-out; presence needs **both** doc-bridge + federation (`docs/async-layers.md`). One stream/page (no periodic fetch). After `.templ`: `make templ && make css`. Tests: temp Turso + ephemeral ports only; SSE reads deadline-bounded; SW never caches `/api/*` (outbox instead). `git stash drop` is destructive — use `pop`/branch. Deploy pulls image by digest; never `scp`/`chown`.

## Security invariants

Every route: session check + `user` row + allowlist entry. Every record: owner-scoped SQL + `canDelete*`; owner via session, never client ID. Every sink: Templ escapes (`Raw` only allowlisted); SQL via sqlc `?`; paths via `os.Root`. Every fetch: operator-config URLs, allowlist + loopback deny; never shell. Every inline script: per-request nonce. Password/TOTP/sign-out-everywhere + admin mutations: step-up proof + audit. Audits carry event/reason/subject/actor, never secrets. Logs in handlers via `internal/observability`. New deps: principles + pinned + `govulncheck`. Security tests must go red (RED/GREEN/PROVE).

## No AI attribution

No `Co-Authored-By` / human trailers on commits, PRs, notes, CHANGELOG. Drafts end at the last meaningful sentence.

## Releases

release-please owns versioning: agents push feature branches + normal PRs only — never release PRs, tags, or version edits. Pre-1.0 stays until a human says so (breaking → 0.x minor bump). Fill every PR-template section (no placeholders, `pr-body` CI enforces). Bot owns `CHANGELOG.md` — never hand-edit. After master merge, tell the user the `chore: release` PR is theirs to review; never merge it yourself.

## Shared-host lint

Never whole-repo lint/tests/`ci-local` on this box (8 GB, co-tenanted) — `make lint-safe` / scoped tests / `make ci-local-fast`, let PR CI be the full gate. Never `cache clean`. `deadcode SKIPPED` means the host gate worked — don't force it; CI runs it.
