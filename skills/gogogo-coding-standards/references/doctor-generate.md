# doctor / generate — when to run them

Two deterministic entrypoints for LLM-era work (ported from `mygo doctor` /
`mygo generate` pattern). Read-only doctor, deterministic generate, no LLM in
the path.

```bash
go run ./cmd/gogogo doctor --json     # preflight for agents/CI
go run ./cmd/gogogo generate --check  # CI gate: exit 1 on drift
go run ./cmd/gogogo generate          # fix drift yourself (templ+sqlc+css+docs)
```

Rules:

- `doctor` is read-only, `--json` stdout pure, logs stderr, exit 0 ok / 4 fail.
  Checks: `go.version`, `go.tool.templ`, `tool.*` (ripwire/sem/cymbal/xerj/lefthook/sqlc/docker; zig warn opt-in), `desktop.sdk` (xcode/GTK warn-only), `mobile.sdk` (skip unless `ANDROID_HOME`), `secrets.age`, `host.capacity`, `registry.fresh`, `generate.drift` (skip → `generate --check`).
- `generate` is idempotent: templ + sqlc + css + docs regen. Committed outputs stay committed; CI regen fails on drift.
- LLM workflow: run `doctor --json` first when starting work; run `generate --check` before opening a PR; if drift, run `generate` and commit the regenerated files.
