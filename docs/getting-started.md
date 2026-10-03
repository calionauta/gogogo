# Getting started

Use the green **Use this template** button on the GitHub repo, or clone it:

```bash
git clone https://github.com/calionauta/gogogo-fullstack-template.git my-project
cd my-project
make dev
```

Open `http://localhost:8080` for the landing page, then
`http://localhost:8080/todo` for the demo — sign in with the seeded
`demo@demo.app` / `demo`.

> The default port is `8080` (override with `PORT`). The default branch is
> `master`.

`make dev` regenerates the Templ components and runs Air for live reload (Air's
`pre_cmd` also runs `bin/datastar-lint`). It does **not** rebuild CSS — run
`make css` when you change Tailwind classes, or `make css-all` for every skin.
It is the only command you need on day one.

## Prerequisites

- **Go 1.27+** — the only hard requirement. Everything else the Makefile
  installs or vendors.
- `make setup` (optional but recommended) — activates the lefthook git hooks so
  formatting, lint, and the CSS staleness check run on every commit. Requires
  `go install github.com/evilmartians/lefthook@latest`.

There is no build-tag matrix. `make build` and `go build ./cmd/web` compile
everything — the unified build era means you never pass `-tags`.

## First five minutes

1. `make dev` — the binary boots with PocketBase + goqite + SSE Hub + DagNats +
   NATS, and seeds the demo user and collections on first run.
2. Open `/` — public landing page, no auth.
3. Open `/todo` — sign in as `demo@demo.app` / `demo`, add a todo, and watch it
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

Secrets live in `~/.secrets/<service>.env`, mode 600, decrypted at boot with
[age](https://age-encryption.org). The Todo example wires an
`ADMIN_UNLOCK_TOKEN` master-password path end-to-end.

```bash
bin/init-secrets
# Generates ~/.secrets/key.txt (mode 600), writes a template at
# ~/.secrets/<project>.env, and encrypts it to <project>.env.age — the file
# the app reads at boot. Then add the printed export to your shell profile:
export AGE_SECRET_KEY=AGE-SECRET-KEY-1...
```

After editing `<project>.env`, re-encrypt with `bin/init-secrets --reencrypt`.
The script needs the `age` CLI, or Go (it falls back to `scripts/agehelper`,
which uses the same `filippo.io/age` library the app already depends on).

Full env-var reference in [Configuration](configuration.md).

## Related

- [Features](features.md) — what you get out of the box, and how to remove it.
- [Scope taxonomy](scope-taxonomy.md) — the rule for deciding what is safe to delete.
- [Deploy to your own box](deploy.md) — when you're ready to ship.