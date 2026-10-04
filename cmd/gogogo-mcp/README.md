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

| Tool | Params | Effect |
|---|---|---|
| `capabilities_list` | — | Full registry JSON (start here) |
| `advise_stack` | `need?`, `format?` | Opinions, not changes: use-case → keep/drop + off switches + Go/Zig rule |
| `trim_plan` | `name`, `owner?`, `dir?`, `plugins?`, `features?`, `format?` | Validates ids, shows consequences. Changes nothing |
| `trim_apply` | trim_plan params + `confirm: true` | Rename + trim + tidy + build proof. Refuses without `confirm:true` |
| `check_tree` | `dir` | `CHECK-OK`/`CHECK-FAIL` drift verdict, changes nothing |
| `add_unit` | `unit`, `from`, `dir`, `confirm`, `dryRun?` | Dependency closure + copy + rewire + proof. Refuses without `confirm:true` (or preview with `dryRun:true`) |

Formats default to `json` on this transport (humans get text on the CLI).
Mutating tools return the same envelopes the CLI prints, including
`buildOk:false` with compiler output instead of silence.

## Security notes (read before enabling)

These tools delete files, rewrite imports, run `go mod tidy`, and execute
`go build` in the given directory — the same power as the CLI, reachable
as structured calls. There is no sandboxing beyond the host OS user:
enable only for checkouts you trust, prefer absolute `--dir` paths, and
never point it at `/`, `$HOME`, or a checkout with uncommitted work you
cannot afford to lose (`git status` clean first is the convention).
Destructive tools fail closed: no `confirm:true` (or `--yes` on the CLI),
no changes — preview tools never mutate.
