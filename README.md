# gogogo

<p align="center">
  <img src="web/resources/static/logo.png" alt="gogogo" width="420">
</p>

<p align="center">
  <a href="https://calionauta.github.io/gogogo/">Landing page</a> ·
  <a href="https://calionauta.github.io/gogogo/docs/">Docs</a> ·
  <a href="https://github.com/calionauta/gogogo/releases">Releases</a> ·
  <a href="https://github.com/calionauta/gogogo/actions">CI</a>
</p>

> **Your stack is ready. Come build.** Every decision favors practical outcomes
> over abstract ideals. The stack optimizes for simplicity, consistency, and
> shipping software with minimal friction.

A Go web template: backend + frontend + DB + auth + LLM + deploy in one
binary. Not a framework — wired, documented code you own and edit.

Full manual: **[Docs](https://calionauta.github.io/gogogo/docs/)**
(`docs/` is the source, `site/` publishes it).

## Quick start

Use this template to create your own copy, then rename it — don't build
inside a clone of the template itself:

```bash
curl -sSfL https://raw.githubusercontent.com/calionauta/gogogo/master/install.sh | sh -s -- --run
# Open http://localhost:8080 (demo: demo1@demo.app / demo1234456)
```

Manual fallback: `gh repo create my-app --template calionauta/gogogo --clone`,
then `make rename NAME=my-app`, `make dev`.

## Stack (snapshot)

Go 1.27 · Templ + Datastar (SSE) + Tailwind/DaisyUI · Turso (sqlc) +
argon2id sessions + Casbin RBAC · goqite + DagNats + NATS JetStream ·
GoAkt (rooms) · Loro CRDT (whiteboard/notes) · GoAI (any provider). One
`make build` compiles everything; opt out at runtime with env vars.

## Recipes

Product slices are recipes: copied code you own, with a receipt
(`.recipes/<id>.yml`) recording where it came from. Install, update,
and uninstall with `gogogo recipe install|update|uninstall` — user edits
are kept (three-way via git). Reference: `features/todo`. Minimal
installable example: `_extensions/pomodoro`. Contract:
[`skills/gogogo-recipe-authoring`](skills/gogogo-recipe-authoring/SKILL.md).
Taxonomy: [scope-taxonomy](docs/scope-taxonomy.md).

## For AI agents

- [`llms.txt`](llms.txt) — what to read per task.
- `go run ./cmd/gogogo advise|doctor|generate|recipe|export|new feature` —
  same engine as the MCP server (`cmd/gogogo-mcp`). See
  [cmd/gogogo/README.md](cmd/gogogo/README.md).

## Try it live

- [Todo & Whiteboard demo](https://gogogo.calionauta.com/)
- [DagNats console](https://gogogo.calionauta.com/dagnats/)

## Commands

```bash
make dev      # live reload
make ci-local # full gate (= CI)
make site     # rebuild the docs site
```

Details: [getting-started](docs/getting-started.md#commands).

## License

MIT — see [LICENSE](LICENSE). Feedback and PRs welcome.
