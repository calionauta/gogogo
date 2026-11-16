# doctor / generate

Two deterministic entrypoints for LLM-era development, modelled after
`mygo doctor` / `mygo generate` and the 2026 OpenClaw/Ralph TUI pattern:
one JSON object `{ok, checks[{id,name,status:pass|warn|fail|skip,version,required,fix}], warnings[], meta:{exit_code,duration_ms}}`,
read-only, `--json` stdout pure, logs stderr, exit 0 ok / 1 fail / 4 usage.

```
go run ./cmd/gogogo doctor            # text summary (0 fail on macOS CLT; zig warn if absent)
go run ./cmd/gogogo doctor --json     # machine-readable for agents/CI
go run ./cmd/gogogo doctor --only go.version --json
```

Checks today: `go.version`, `go.tool.templ` (pinned), `tool.*` (ripwire, sem, cymbal, xerj, lefthook, sqlc, docker, zig warn opt-in, mygo warn opt-in — packaging only), `desktop.sdk` (xcode/GTK), `mobile.sdk` (skip unless ANDROID_HOME), `secrets.age`, `host.capacity`, `registry.fresh`, `generate.drift`, `recipe.declarations` (drift, schema, policy, wiring, metrics, panels, tests, docs, complexity).

`generate.drift` is a real read-only check: it compares each `.templ` against the
`_templ.go` beside it and each `internal/db/queries` file against the newest
`sqlcgen` output, and warns when a source is newer. It does not regenerate — a
preflight must be safe to run mid-edit. It was a permanent `skip` until this
change, which made `doctor` report `ok: true` while claiming a check it never
performed.

`recipe check` is the recipe evaluator itself (`gogogo recipe check [id] --dir . --format json`); doctor calls the same code as one preflight item, and `make check-recipes` runs it in `ci-local-fast` and `ci-local`.

```
go run ./cmd/gogogo generate          # templ + sqlc + css + docs, idempotent
go run ./cmd/gogogo generate --check  # CI gate: exit 1 on drift (git diff)
go run ./cmd/gogogo generate --only templ,sqlc,docs
```

`generate` mirrors Solarch/ArchRad: same input → byte-identical output, committed
outputs (templ/sqlc/css/docs). No LLM in the path (deterministic pure).

`--check` answers ONE question: did running the steps change the files they own
(`generateOutputs`: `internal/db/sqlcgen`, `web/resources/static/app.min.css`,
`docs/api-routes.md`, `docs/unit-contracts.md`, `*_templ.go`)? It hashes them
before and after, so a branch with unrelated edits does not report "drift" —
which is what `git diff --exit-code` did, making the gate useless exactly when
anyone ran it.
