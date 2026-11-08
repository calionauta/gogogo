# doctor / generate

Two deterministic entrypoints for LLM-era development, modelled after
`mygo doctor` / `mygo generate` and the 2026 OpenClaw/Ralph TUI pattern:
one JSON object `{ok, checks[{id,name,status:pass|warn|fail|skip,version,required,fix}], warnings[], meta:{exit_code,duration_ms}}`,
read-only, `--json` stdout pure, logs stderr, exit 0 ok / 1 fail / 4 usage.

```
go run ./cmd/gogogo doctor            # text summary (2 fails = zig + xcode here)
go run ./cmd/gogogo doctor --json     # machine-readable for agents/CI
go run ./cmd/gogogo doctor --only go.version --json
```

Checks today: `go.version`, `go.tool.templ` (pinned), `tool.*` (ripwire, sem, cymbal, xerj, lefthook, sqlc, docker, zig), `desktop.sdk` (xcode/GTK), `mobile.sdk` (skip unless ANDROID_HOME), `secrets.age`, `host.capacity`, `registry.fresh`, `generate.drift` (skip → `generate --check`).

```
go run ./cmd/gogogo generate          # templ + sqlc + css + docs, idempotent
go run ./cmd/gogogo generate --check  # CI gate: exit 1 on drift (git diff)
go run ./cmd/gogogo generate --only templ,sqlc,docs
```

`generate` mirrors Solarch/ArchRad: same input → byte-identical output, headers with `spec_hash/template_hash/version/rendered_at`, committed outputs (templ/sqlc/css/docs) + CI regen fails on drift. No LLM in the path (deterministic pure).
