# MCP server

Drive the installer with tool calls instead of shell. Same engine as the
CLI (`internal/installer`), same validation, receipts, and proof — the
transports never diverge.

## Install

`install.sh` already puts `gogogo-mcp` next to `gogogo`:

```bash
curl -sSfL https://raw.githubusercontent.com/calionauta/gogogo/master/install.sh | sh -s -- --run
```

Or build from source (needs Go):

```bash
cd cmd/gogogo-mcp && go build -o gogogo-mcp .
```

Check it: `gogogo-mcp --help` prints the catalog.

## Wire it into your client

The server speaks MCP over stdio: no port, no daemon, no deploy. Your
agent client spawns the binary per session. Add one block to the client
config (adjust the path if you installed elsewhere — default below is
where `install.sh` puts it):

```json
{ "mcpServers": { "gogogo": { "command": "~/.local/bin/gogogo-mcp" } } }
```

- **Claude Code** — user config (`~/.claude.json`) or `claude mcp add`.
- **Cursor** — workspace/client config (`.cursor/mcp.json`).
- **BB** — client MCP config, same block.

## Tools

| Tool | Changes anything? | What it does |
|---|---|---|
| `advise_stack` | No | Opinions: which units fit a use-case, off-switches, Go/Zig rule. Start here |
| `capabilities_list` | No | Full capability registry JSON |
| `trim_plan` | No | Preview a scaffold/trim (validates ids, shows consequences) |
| `trim_apply` | Yes (`confirm:true`) | Rename + trim + tidy + build proof |
| `check_tree` | No | Marker drift verdict per unit |
| `add_unit` | Yes (`confirm:true`) | Restore a unit (`dryRun:true` previews) |

Read-only tools need nothing else. Mutating tools fail closed without
`confirm:true` — preview with `trim_plan` (or `dryRun:true`) first, then
apply. Formats default to `json` on this transport.

Generated code follows `skills/gogogo-coding-standards/SKILL.md`
(Go rules; `advise_stack` output cites it).

## Pattern

1. `advise_stack` with the use-case in plain words (empty `need` returns
   the full map).
2. `trim_plan` to preview the consequences.
3. `trim_apply` with `confirm:true` to execute.

Full catalog with parameter shapes: [`cmd/gogogo-mcp`](https://github.com/calionauta/gogogo/tree/master/cmd/gogogo-mcp).
Agent strategies and the scripted CLI equivalent: [getting-started](getting-started.md#0b-opinions-before-changes-advise).
