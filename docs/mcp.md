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
| `advise_stack` | No | Opinions **and the route**: which units fit a use-case, off-switches, and the ordered commands for `intent` (`update`/`port`/`add-unit`/`new-feature`/`scaffold`). Optional `dir` adds the same probe as the CLI's `--dir`. Answers in one of four scopes — see below. Start here |
| `capabilities_list` | No | Full capability registry JSON |
| `trim_plan` | No | Preview a scaffold/trim (validates ids, shows consequences) |
| `trim_apply` | Yes (`confirm:true`) | Rename + trim + tidy + build proof |
| `check_tree` | No | Marker drift verdict per unit, plus the base revision the tree records |
| `version_status` | No | Is this tool — and this project's recorded base — behind the latest release? Pass `dir` for the project half. "An update exists" is a verdict, not an error |
| `migrate_plan` | No | Releases between the project's recorded base and the latest, plus the gates that prove a migrated tree. Pass `files:true` to add the per-file impact, labelled by capability and by whether this tree kept the owning unit — the "which of these apply to me" half |
| `add_unit` | Yes (`confirm:true`) | Restore a unit (`dryRun:true` previews) |
| `export_unit` | Only writes `to` | Port bundle for a tree that is **not** a gogogo checkout: copies the unit's owned files into `to` + writes `PORTING.md` (routes, imports to rewrite, what to write by hand, proof). `dir` is never touched |
| `new_feature` | Yes (`confirm:true`) | Scaffold one feature (handler + page + wiring + unit + RBAC rows) and prove the gates (`dryRun:true` previews) |

Read-only tools need nothing else. Mutating tools fail closed without
`confirm:true` — preview with `trim_plan` (or `dryRun:true`) first, then
apply. `export_unit` is the exception to the exception: it mutates only the
scratch directory you name, never the checkout it reads, so it needs no
confirm gate. Formats default to `json` on this transport.

Generated code follows [`skills/gogogo-coding-standards`](https://github.com/calionauta/gogogo/tree/master/skills/gogogo-coding-standards)
(Go rules; `advise_stack` output cites it).

### The four `advise_stack` scopes

Three are chosen from `need` alone; the fourth is the opt-in `dir` probe.
Check the `scope` field before acting on the rest. The tool knows the template
plus its one native exception — never a stack label:

| `scope` | When | What the rest of the document contains |
|---|---|---|
| `template` | a gogogo-shaped need — or anything the vocabulary does not match (full map shown instead of a guess; scaffold first-run withheld when nothing matched) | the full capabilities table + keep/drop presets — act on these |
| `exception-to-go` | the need names **Zig** | the exception-to-go gate + skill pointer; **nothing installs** under the exception |
| `go-standards` | a need that forbids dependencies ("stdlib only", "no deps") | the Go-standards pointer only; the capability table is **absent**, not empty |
| `foreign-tree` | `dir` points at a path that is not a gogogo checkout | the capability table with its owned paths (what to **copy**), the not-checkout rules, and the route to `export_unit` + the porting checklist |

On `go-standards`, do not read the absence of capabilities as "no opinion" —
the rules say why the template cannot apply and where the Go standards live.
For an `exception-to-go` need, apply the gate (Go baseline + profile first) and
read the referenced skill; do not try to install template units.

On `foreign-tree`, nothing installs: `add_unit` and `new_feature` rewire a
scaffold's own router and registry, and they refuse rather than guess at
yours. Read `capabilities[].dirs/files` as a shopping list, then `export_unit`
writes the bundle and the wiring recipe.

### `intent`: the route, stated or inferred

The document carries `intent` + `intentSource` and a `next` array of ordered
commands. `intentSource` is `given` (you passed `intent`), `detected` (the
wording implied it — "keep this up to date" is `update`) or `default` (a
non-checkout `dir` leaves porting as the only step that can work). An absent
intent means no route: the tool does not guess one. Pass `intent` whenever you
know the goal — the model reads the user better than any keyword table, and a
stated intent is never overruled by the wording.

### check_tree needs a gogogo checkout

`check_tree` verifies the trim manifest against a scaffolded tree. Run it on an
unrelated project and it now returns a single `CHECK-FAIL (tree) not a gogogo
checkout` line rather than one failure per missing marker, so a wrong-repo call
is distinguishable from a real drift problem.

### `version_status` is not a nag

Nothing here phones home unless the agent calls this tool: there is no startup
check and no timer, because a stdio server that blocks on the network at boot
turns slow DNS into a broken session. Pass `dir` — without it only the tool's
own release is compared, which reports a months-old tree as current.

The answer is JSON (`installed`, `latest`, `tree`, `treeStatus`, `source`) and an
existing update comes back as a normal result, never as a tool error. To turn
the verdict into work, use the CLI's `gogogo migrate` — see
[Upgrading](upgrading.md).

## Pattern

1. `advise_stack` with the use-case in plain words (empty `need` returns
   the full map).
2. `trim_plan` to preview the consequences.
3. `trim_apply` with `confirm:true` to execute.

Full catalog with parameter shapes: [`cmd/gogogo-mcp`](https://github.com/calionauta/gogogo/tree/master/cmd/gogogo-mcp).
Agent strategies and the scripted CLI equivalent: [getting-started](getting-started.md#0b-opinions-before-changes-advise).
