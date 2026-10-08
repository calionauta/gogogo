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

That JSON shape fits Claude Code (`.mcp.json` per project, or
`claude mcp add gogogo -- ~/.local/bin/gogogo-mcp` for just you).
The others need their own format — same binary, same tools:

- **Claude Code** — `.mcp.json` above, or the `claude mcp add` command.
  Verify: `claude mcp list`, then restart.
- **Codex** — TOML in `~/.codex/config.toml` (every project) or
  `.codex/config.toml` (this project, must be trusted):
  `[mcp_servers.gogogo]` + `command = "~/.local/bin/gogogo-mcp"`.
  Verify: `/mcp` in session, or `codex mcp list`.
- **OpenCode** — `opencode mcp add gogogo -- ~/.local/bin/gogogo-mcp`,
  or `"mcp": {"gogogo": {"type": "local", "command": [...]}}` in
  `opencode.json`. Verify: `opencode mcp list`.
- **Pi** (1.0) — `pi mcp add gogogo -- ~/.local/bin/gogogo-mcp`.
  Reads the standard `mcpServers` format too. Verify: `pi mcp list`.

Use the absolute path (`echo $HOME`) — most clients spawn without a
shell, so `~` may not expand. Cursor, VS Code, BB and other
JSON-config clients take the Claude Code block verbatim.

## Tools

| Tool | Changes anything? | What it does |
|---|---|---|
| `advise_stack` | No | Opinions: which units fit a use-case, off-switches, Go/Zig rule. Optional `dir` adds the same probe as the CLI's `--dir`. Answers in one of three scopes — see below. Start here |
| `capabilities_list` | No | Full capability registry JSON |
| `trim_plan` | No | Preview a scaffold/trim (validates ids, shows consequences) |
| `trim_apply` | Yes (`confirm:true`) | Rename + trim + tidy + build proof |
| `check_tree` | No | Marker drift verdict per unit |
| `add_unit` | Yes (`confirm:true`) | Restore a unit (`dryRun:true` previews) |

Read-only tools need nothing else. Mutating tools fail closed without
`confirm:true` — preview with `trim_plan` (or `dryRun:true`) first, then
apply. Formats default to `json` on this transport.

Generated code follows [`skills/gogogo-coding-standards`](https://github.com/calionauta/gogogo/tree/master/skills/gogogo-coding-standards)
(Go rules; `advise_stack` output cites it).

### The three `advise_stack` scopes

Chosen from `need` alone. Check the `scope` field before acting on the rest.
The tool knows the template plus its one native exception — never a stack
label:

| `scope` | When | What the rest of the document contains |
|---|---|---|
| `template` | a gogogo-shaped need — or anything the vocabulary does not match (full map shown instead of a guess; scaffold first-run withheld when nothing matched) | the 24 capabilities + keep/drop presets — act on these |
| `exception-to-go` | the need names **Zig** | the exception-to-go gate + skill pointer; **nothing installs** under the exception |
| `go-standards` | a need that forbids dependencies ("stdlib only", "no deps") | the Go-standards pointer only; the capability table is **absent**, not empty |

On `go-standards`, do not read the absence of capabilities as "no opinion" —
the rules say why the template cannot apply and where the Go standards live.
For an `exception-to-go` need, apply the gate (Go baseline + profile first) and
read the referenced skill; do not try to install template units.

### check_tree needs a gogogo checkout

`check_tree` verifies the trim manifest against a scaffolded tree. Run it on an
unrelated project and it now returns a single `CHECK-FAIL (tree) not a gogogo
checkout` line rather than one failure per missing marker, so a wrong-repo call
is distinguishable from a real drift problem.

## Pattern

1. `advise_stack` with the use-case in plain words (empty `need` returns
   the full map).
2. `trim_plan` to preview the consequences.
3. `trim_apply` with `confirm:true` to execute.

Full catalog with parameter shapes: [`cmd/gogogo-mcp`](https://github.com/calionauta/gogogo/tree/master/cmd/gogogo-mcp).
Agent strategies and the scripted CLI equivalent: [getting-started](getting-started.md#0b-opinions-before-changes-advise).
