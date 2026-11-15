# doctor / generate — when to run them

Two deterministic entrypoints for LLM-era work (ported from `mygo doctor` /
`mygo generate` pattern). Read-only doctor, deterministic generate, no LLM in
the path.

```bash
go run ./cmd/gogogo doctor --json     # preflight for agents/CI
go run ./cmd/gogogo generate --check  # CI gate: exit 1 when generation CHANGED an owned file
go run ./cmd/gogogo generate          # fix drift yourself (templ+sqlc+css+docs)
```

Rules:

- `doctor` is read-only, `--json` stdout pure, logs stderr, exit 0 ok / 1 fail / 4 usage.
  Checks: `go.version`, `go.tool.templ`, `tool.*` (ripwire/sem/cymbal/xerj/lefthook/sqlc/docker; zig warn opt-in), `tool.mygo` (warn opt-in — packaging only, `make mygo` needs no CLI), `desktop.sdk` (xcode/GTK warn-only), `mobile.sdk` (skip unless `ANDROID_HOME`), `secrets.age`, `host.capacity`, `registry.fresh`, `generate.drift` (real: warns when a `.templ` is newer than its `_templ.go`, or a query newer than `sqlcgen`; read-only, no regeneration).
- `generate` is idempotent: templ + sqlc + css + docs regen. Committed outputs stay committed.
- `--check` hashes the files the steps OWN (`generateOutputs`) before and after, so an unrelated branch edit does not read as drift. (It used to run `git diff --exit-code` with no paths — the answer to "is my tree dirty", which is always yes while working.)
- LLM workflow: run `doctor --json` first when starting work; run `generate --check` before opening a PR; if drift, run `generate` and commit the regenerated files.
