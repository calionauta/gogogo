# Getting started

This repository is a **GitHub template**. The recommended path is the guided
installer — it asks for a project name, clones the template, renames it, and
trims the plugins/features you skip. Manual `gh repo create` + `make rename`
still works and is documented below as the fallback.

## 0. Recommended: the installer

```bash
go run github.com/calionauta/gogogo/cmd/gogogo@latest
cd my-app && make dev
```

No checkout needed: when the directory is missing the installer clones
the template into it first (asks, or `--yes` to proceed unattended).
No Go on this machine? Use the [binary release](../README.md#cli--mcp-agent-paths)
instead of `go run` — same installer, no toolchain needed to run it.
What it asks (4 questions):

1. **Project name** (`my-app`) — validates like `make rename` does.
2. **GitHub owner** — for the module path (`github.com/<owner>/<name>`).
3. **Plugins** — numbered menu from the registry (`dagnats`,
   `credits`, `sounds`, `skins-extra` with one-line summaries). Answer
   with numbers (`1,3`), ids, or both; empty keeps all, `none` drops all.
   Unknown answers re-ask instead of mis-trimming. Plugin answers match
   plugin-kind units only.
4. **Features** — same numbered-menu shape (`whiteboard`, `landing`,
   `config-view`). The todo demo is never offered (it is the
   reference implementation). Realtime (NATS), LLM, and offline sync are
   not installer units: they disable at runtime via env
   (`NATS_ENABLED=false`, unset `GOAI_API_KEY`,
   `OFFLINE_SYNC_ENABLED=false`).

What it does, in order: shows the trim plan with every consequence (never
silent), and on confirmation clones the template when the directory is
missing, deletes what you skipped, renames (same rules
as `scripts/rename-project.py`), writes `AGENTS.md`, then proves it with
`go tool templ generate` (when `.templ` changed) + `go mod tidy` +
`go build ./cmd/web`, and prints the exact next commands
(`cd <name> && make dev` — a child process cannot `cd` its parent, which
is why every scaffolder from `create-vite` to `create-t3-app` prints the
`cd` instead of doing it).
Trim details: [Scope taxonomy](scope-taxonomy.md#removing-a-component).

Non-interactive (CI/scripted) — preview first, then pin `--yes`:

```bash
go run ./cmd/gogogo --name my-app --owner myorg \
  --plugins dagnats,credits,sounds --features whiteboard,landing,config-view \
  --no-tui --dry-run --format json --dir ./my-app
go run ./cmd/gogogo --name my-app --owner myorg \
  --plugins dagnats,credits,sounds --features whiteboard,landing,config-view \
  --no-tui --yes --dir ./my-app
```

IDs must match `cmd/gogogo --help` (unknown ids fail fast instead of
silently keeping everything). Use `none` to drop a whole dimension.

`--no-tui` without `--yes` prints the plan and stops — the installer never
deletes on an assumption. Exit codes: 0 ok/plan-only, 1 usage error,
2 proof build failed.

TUI tech: the installer is stdlib-only today so `go run` needs nothing else.
The interactive form is structured so it can move to Charm `Huh` + `Bubbletea`
(a 2026-standard, performative Go TUI stack) without changing the trim engine —
see `cmd/gogogo/README.md`.

On success the installer prints the exact next commands — it cannot run
them for you (`go run @latest` executes from an ephemeral module, so the new
project's dev loop stays yours):

```text
gogogo: done — next:
  cd my-app && make dev
  app:       http://localhost:8080 (PORT overrides)
  login:     demo@demo.app / demo1234456 (prefilled on the sign-in form)
  admin:     http://localhost:8080/_/ (PocketBase — create the superuser on first visit)
  workflows: http://localhost:8080/dagnats/ (DagNats console)
```

## 0b. Opinions before changes: `advise`

When you (or your agent) want guidance instead of a scaffold — which units
for which use-case, how each capability switches off, whether Zig is ever
justified — ask first, install nothing:

```bash
go run github.com/calionauta/gogogo/cmd/gogogo@latest advise --need "offline-first todo with AI"
go run github.com/calionauta/gogogo/cmd/gogogo@latest advise --need "realtime whiteboard" --format json
```

No `--dir`, no `--yes`, no filesystem touched: it reads the capability
registry and prints use-case presets (keep/drop per preset), every
capability with its trim flag or runtime off-switch, and the three global
rules (Go-first with the profiled-kernel Zig exception, runtime switches
before trim, upstream-first). The MCP server exposes the same document as
`advise_stack` — agents deciding what to use start there, not at `trim_plan`.

Strategy for LLMs: empty `--need` returns the full map (cheapest correct
first call — the document is small). A filtered call that matches nothing
is not a dead end: the capabilities table is always complete, so decide
from it or retry with broader terms. Keyword matching is deliberately
dumb (exact or ≥4-char prefix) — phrase the need with template vocabulary
(`whiteboard`, `dagnats`, `offline`, `credits`) when a first attempt
misses. Then preview with `trim_plan --dry-run` before any `trim_apply`.

## 0c. Adding to an existing project

Two cases, sharply different:

**Scaffolded checkout (this template, already trimmed or not).**
`add` restores one unit back with its dependency closure, module-path
rebase, and proof build:

```bash
go run ./cmd/gogogo add whiteboard --from ~/gogogo --dir ./my-app --dry-run
go run ./cmd/gogogo add whiteboard --from ~/gogogo --dir ./my-app --yes
```

`--from` is a pristine template checkout to copy from; `--check --dir`
verifies the markers first. `add` refuses anything that is not a
scaffolded checkout (`router/router.go` + `go.mod` must exist) — it
restores known markers, it does not merge foreign code.

**Foreign Go codebase (not scaffolded).** There is no auto-adoption:
the installer cannot know your router, module layout, or auth, so it
will not guess. Do instead:

1. Run `advise` (above) for the opinionated shortlist.
2. Read the upstream pattern via `llms.txt` (`AGENTS.md` has the map) —
   Todo is the reference implementation for jobs, SSE, and realtime.
3. Copy the capability's dirs (the registry lists owned paths per
   capability) and wire the single call in your router.

## 1. Manual fallback: create your repository

```bash
# GitHub UI: click "Use this template" -> "Create a new repository"
# or from the CLI:
gh repo create my-app --template calionauta/gogogo --clone
cd my-app
```

## 2. Rename the project

A fresh copy still says `gogogo` in ~360 places across ~130
files (module path, binary name, container, titles, deploy paths). One command
rewrites all of it and then builds to prove it worked:

```bash
make rename NAME=my-app            # module path + every reference
make rename NAME=my-app OWNER=myorg  # if the repo is not under calionauta
make rename NAME=my-app DRY=1      # preview without writing
```

Then regenerate the published docs, which are built from the markdown:

```bash
make site                          # rewrites site/docs/, llms.txt, sitemap.xml
```

> The rename touches **only this project's** identity. Sibling repositories
> under the same owner (`ai-credits`, `datastar-lint`, `pi-leakguard`) are real
> dependencies and are left untouched — the script shields them explicitly.

## 3. Run it

```bash
make dev
```

Open `http://localhost:8080` for the landing page, then
`http://localhost:8080/todo` for the demo — sign in with the seeded
`demo@demo.app` / `demo1234456`.

> The default port is `8080` (override with `PORT`). The default branch is
> `master`.

`make dev` regenerates the Templ components and runs Air for live reload (Air's
`pre_cmd` also runs `bin/datastar-lint`). It does **not** rebuild CSS — run
`make css` when you change Tailwind classes, or `make css-all` for every skin.
It is the only command you need on day one.

## Prerequisites

- **Go** — any release ≥ 1.21. Newer toolchains download themselves
  (`GOTOOLCHAIN=auto`), so the template's 1.27 is satisfied automatically.
- **git** — the installer clones the template when the directory is missing.

The installer verifies both before touching anything and fails fast with
the exact install command when something is missing. `make setup` (optional
but recommended) — activates the lefthook git hooks so
formatting, lint, and the CSS staleness check run on every commit. Requires
`go install github.com/evilmartians/lefthook@latest`.

There is no build-tag matrix. `make build` and `go build ./cmd/web` compile
everything — the unified build era means you never pass `-tags`.

## First five minutes

1. `make dev` — the binary boots with PocketBase + goqite + SSE Hub + DagNats +
   NATS, and seeds the demo user and collections on first run.
2. Open `/` — public landing page, no auth.
3. Open `/todo` — sign in as `demo@demo.app` / `demo1234456`, add a todo, and watch it
   stream through PocketBase realtime.
4. Open `/config` — auth-gated read-only view of what the binary decided:
   env-decrypted values, masked secrets, runtime constants.
5. Open `/whiteboard` — collaborative canvas; open it in a second window to
   see presence cursors and CRDT convergence.

## Commands

```bash
make build         # Build binary (unified: everything included)
make dev           # Live reload with Air (also re-runs templ + vet)
make templ         # Regenerate .templ Go files after a .templ edit
make css           # Rebuild app.min.css from src/css/input.css
make lint          # go vet + golangci-lint (27 linters), full repo
make datastar-lint # Datastar attribute / signal anti-patterns in .templ
make fmt           # gofumpt + goimports check (CI gate; apply via gofumpt -w)
make test          # Race tests (`-p 1` for DagNats engine stability)
make ci-local      # Full local gate (= CI): templ + datastar-lint + css-check + golangci-lint + race tests + build
make gui           # Native gogpu/ui PoC: headless race tests + CGO_ENABLED=0 build
make run-gui       # Open the native window (needs DISPLAY/GPU; not exercised in CI)
make signoff       # `make ci-local` + `gh signoff -f` stamp. Default pre-push gate
make setup         # Activate lefthook git hooks
make docker-image  # Build and push multi-arch image to ghcr.io
```

> `make check` was removed — it was a redundant subset of `make ci-local`. If a
> doc or muscle memory mentions it, use `make ci-local`.

`make test` is discouraged locally: the remote CI runs the same suite and it
takes minutes. For fast feedback while iterating, scope the tests:
`go test -race -count=1 ./features/todo/...`.

## Adding your own feature

Every feature follows the same pattern — use the Todo feature as the reference
implementation.

1. Create `features/<name>/` with its HTTP handlers + Templ components.
2. Wire it in `router/router.go` → `Init()` with a single function call.
3. Use **goqite** for async work, the **SSE Hub** for user-facing feedback
   (toasts, progress), and **Datastar** for the reactive frontend.
4. Add a `SCOPE:` annotation so agents know what they can remove —
   `make check-scope` enforces it.
5. Add a `RegisterRoutes(se, deps)` function and call it from `router.Init`.

The contract to imitate, in four rules:

1. **Pure HTTP + Datastar** for the user-facing surface.
2. **goqite job** for any work that takes more than ~50ms (LLM, email, exports).
3. **SSE toast** for async feedback to the originating client via `clientID`
   routing.
4. **age-encrypted secret** if the feature needs a credential.

Every existing feature — toast on create, AI suggest, admin unlock, DagNats
onboarding — follows this exact shape.

## Secrets setup

Secrets are read from the environment. For local development there is a helper
that keeps them encrypted at rest with [age](https://age-encryption.org) and
decrypts them into the process environment at boot. **Production does not use
age** — see [Production](#production-secrets-come-from-github) below.

### Two files, two roles

The helper writes two files in `~/.secrets/`. They are not interchangeable, and
only one of them is read by the app:

| File | Mode | Role |
|------|------|------|
| `<project>.env.age` | 600 | **Encrypted. This is the file the app reads at boot.** |
| `<project>.env` | 600 | Plaintext working copy. Source for `--reencrypt`. Not read by the app. |
| `key.txt` | 600 | The age identity (secret key). Never committed. |

The directory itself is mode 700.

```bash
bin/init-secrets
# Generates ~/.secrets/key.txt, writes a template at ~/.secrets/<project>.env,
# and encrypts it to <project>.env.age. Then add the printed export to your
# shell profile:
export AGE_SECRET_KEY=AGE-SECRET-KEY-1...
```

Edit `<project>.env`, then re-encrypt:

```bash
bin/init-secrets --reencrypt
```

`secrets.Load()` runs from `config.Load()`, before anything else reads the
environment: it decrypts `<project>.env.age`, parses `KEY=value` lines, and
re-exports them with `os.Setenv` so the rest of the app sees them via
`os.Getenv`. The project name must match what the app derives from `APP_NAME`
(or the binary name) — otherwise it looks for a different file and boots with no
secrets.

> **The plaintext trade-off.** Keeping `<project>.env` in plaintext is what makes
> `--reencrypt` possible without an editor that understands the encrypted
> format. It is protected by mode 600 inside a mode 700 directory, and it never
> goes to git (`.gitignore` covers `.env`). If you would rather never have
> plaintext on disk, delete the working copy after encrypting and edit by
> decrypting to a temp file — or use
> [SOPS](https://github.com/getsops/sops), which edits the encrypted file
> in place. SOPS is deliberately not a dependency here: it is a large module
> and its `dotenv` store has known round-trip bugs with multi-line values
> ([#965](https://github.com/getsops/sops/issues/965),
> [#1435](https://github.com/getsops/sops/issues/1435)).

`bin/init-secrets` uses the `age` CLI when installed, and otherwise falls back to
`scripts/agehelper`, which uses the same `filippo.io/age` library the app
already depends on — so Go alone is enough.

### Production: secrets come from GitHub

**age is a local-development mechanism only.** In production the secrets file is
rendered on every deploy from GitHub Actions secrets and consumed by the
container through Compose's `env_file` — the encrypted file is not involved.

```
GitHub Actions secrets
  → written to /home/deploy/services/<app>/secrets/<app>.env (mode 600, umask 077)
  → read by the container via deploy/docker-compose.prod.yml: env_file
```

So there is no age key to manage on the server, and nothing to rotate there: the
runner is the vault. `AGE_SECRET_KEY` is only needed on a machine doing local
development.

Full env-var reference in [Configuration](configuration.md).

## Related

- [Features](features.md) — what you get out of the box, and how to remove it.
- [Scope taxonomy](scope-taxonomy.md) — the rule for deciding what is safe to delete.
- [Deploy to your own box](deploy.md) — when you're ready to ship.