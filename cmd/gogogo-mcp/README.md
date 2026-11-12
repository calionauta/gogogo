# `cmd/gogogo-mcp` — installer as MCP tools (own module)

Thin adapter over `internal/installer`: every tool maps to the same code
path the CLI executes, so transports never diverge in behavior. Separate
`go.mod` on purpose — the MCP SDK (and its 40+ transitive imports) must
never enter the web binary's module graph.

```bash
cd cmd/gogogo-mcp
go build -o gogogo-mcp .
```

## Client wiring (the user runs it, nobody implements anything)

The user adds one JSON block to their agent client; the client spawns the
binary over stdio per session. No server to host, no port, no deploy.

Claude Code (`~/.claude.json`) / Cursor (`~/.cursor/mcp.json`) / BB:

```json
{ "mcpServers": { "gogogo": { "command": "/path/to/gogogo-mcp" } } }
```

`gogogo-mcp --help` prints the same catalog.

## Tools

<!-- MCP:TOOLS:START -->
| Tool | Params | Effect |
|---|---|---|
| `capabilities_list` | — | No |
| `build_prerequisites` | `platform?` | No |
| `sync_prerequisites` | — | No |
| `version_status` | `dir?`, `format?` | No |
| `trim_plan` | `name`, `owner?`, `dir?`, `format?` | No |
| `trim_apply` | `name`, `owner?`, `dir?`, `format?`, `confirm: true` | Yes (`confirm:true`) |
| `migrate_plan` | `from?`, `to?`, `dir?`, `changelog?`, `files?`, `format?` | No |
| `export_unit` | `unit`, `dir`, `to`, `module?`, `format?` | Only writes `to` |
| `new_feature` | `name`, `dir`, `confirm: true`, `dryRun?` | Yes (`confirm:true`) |
| `recipe_install` | `src`, `dir`, `confirm: true`, `dryRun?`, `entry?`, `format?` | Yes (`confirm:true`) |
| `recipe_update` | `src`, `dir`, `confirm: true`, `dryRun?`, `entry?`, `format?` | Yes (`confirm:true`) |
| `recipe_uninstall` | `id`, `dir`, `confirm: true`, `format?` | Yes (`confirm:true`) |
| `recipe_status` | `dir`, `id?`, `catalog?`, `format?` | No |
| `recipe_outdated` | `dir`, `id?`, `catalog?`, `format?` | No |
| `recipe_list` | `dir?`, `format?` | No |
| `recipe_describe` | `id`, `dir?`, `format?` | No |
| `recipe_check` | `id?`, `dir`, `format?` | No |
| `recipe_test` | `id?`, `dir`, `noRace?`, `run?`, `list?` | No (runs the recipe's own tests) |
| `recipe_invoke` | `id`, `verb`, `server`, `session`, `params?`, `dir?`, `format?` | Server state (mutating verbs only) |
| `advise_stack` | `need?`, `intent?`, `dir?`, `format?` | No |
<!-- MCP:TOOLS:END -->

The table is **generated** from `catalog.go`, the file the server serves from:
the last column is what each tool changes, and the full description of every
tool is in [docs/mcp.md](../../docs/mcp.md#tools). Change the catalog, run
`make docs-index`, and both tables follow.

Formats default to `json` on this transport (humans get text on the CLI).
Mutating tools return the same envelopes the CLI prints, including
`buildOk:false` with compiler output instead of silence.

Generated code follows `skills/gogogo-coding-standards/SKILL.md`
(Go rules; `advise_stack` output cites it).

## Installer surface, not product surface

This server is for the developer evolving a checkout. Some tools rewrite the
tree (`trim_apply`, `new_feature`, `recipe_install|update|uninstall`). Never
ship it to a product's users or their agents. A product's client surface is
the running server's API, including `POST /api/ext/invoke`, under its own
session/RBAC. `recipe_invoke` is a client of that API, not a second product
MCP; it stays loopback-only until a machine identity exists
([docs/mcp.md](../../docs/mcp.md#machine-identity-design-gate-not-implemented)).

## Security notes (read before enabling)

These tools delete files, rewrite imports, run `go mod tidy`, and execute
`go build` in the given directory — the same power as the CLI, reachable
as structured calls. There is no sandboxing beyond the host OS user:
enable only for checkouts you trust, prefer absolute `--dir` paths, and
never point it at `/`, `$HOME`, or a checkout with uncommitted work you
cannot afford to lose (`git status` clean first is the convention).
Destructive tools fail closed: no `confirm:true` (or `--yes` on the CLI),
no changes — preview tools never mutate.
