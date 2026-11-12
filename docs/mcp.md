# MCP server

Drive the installer with tool calls instead of shell. Same engine as the
CLI (`internal/installer`), same validation, receipts, and proof — the
transports never diverge.

This is the **evolution MCP**: it reads and rewrites a checkout. A product's
users and their agents call the running server's routes (including
`POST /api/ext/invoke`) under its session and policy; never ship this server
to them.

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
| `advise_stack` | No | Opinions **and the route**: which capabilities fit a use-case, off-switches, and the ordered commands for `intent` (`update`/`port`/`new-feature`/`scaffold`). Optional `dir` adds the same probe as the CLI's `--dir`. Answers in one of four scopes — see below. Start here |
| `capabilities_list` | No | Full capability registry JSON |
| `trim_plan` | No | Preview a scaffold (validates name/owner, shows the plan); changes nothing |
| `trim_apply` | Yes (`confirm:true`) | Rename + tidy + build proof |
| `version_status` | No | Is this tool — and this project's recorded base — behind the latest release? Pass `dir` for the project half. "An update exists" is a verdict, not an error |
| `migrate_plan` | No | Releases between the project's recorded base and the latest, plus the gates that prove a migrated tree. Pass `files:true` to add the per-file impact, labelled by capability and by whether this tree kept the owning unit — the "which of these apply to me" half |
| `export_unit` | Only writes `to` | Port bundle for a tree that is **not** a gogogo checkout: copies the unit's owned files into `to` + writes `PORTING.md` (routes, imports to rewrite, what to write by hand, proof). `dir` is never touched |
| `new_feature` | Yes (`confirm:true`) | Scaffold one feature (handler + page + wiring + capability unit + RBAC rows) and prove the gates (`dryRun:true` previews) |
| `recipe_install` | Yes (`confirm:true`) | Install a recipe into a gogogo checkout: copy, receipt, generated registration, schema/policy/codegen and proof (dry-run previews) |
| `recipe_update` | Yes (`confirm:true`) | Update one installed recipe from a directory or catalog entry (dry-run previews) |
| `recipe_uninstall` | Yes (`confirm:true`) | Remove one installed recipe by id; deletes only pristine files and keeps edited files |
| `recipe_status` | No | Read-only recipe drift report: pristine vs edited files, generated/schema/policy state |
| `recipe_list` | No | Recipe discovery: built-in capabilities plus user-installed recipes, with origin/lifecycle, summary, invocable verbs, and the mutating routes each recipe deliberately omits |
| `recipe_describe` | No | Describe one recipe by id: kind, origin, summary, registration surface, dependencies, the declared surface, invocable verbs with a description, `{id}`/body params with per-param help, the metrics and outcomes it already declares (job, beneficiary, counter signals), and the routes it deliberately omits, with the reason |
| `recipe_check` | No | Read-only recipe evaluator — the report `gogogo recipe check` prints: healthy flag, score, errors, warnings, suggestions, complexity, the declared metrics/panel cross-check, the derived test inventory and the headless-surface warnings. A blocked recipe comes back as a failed call carrying the report: the verdict is the point, and the transcript says which rule blocked it |
| `recipe_test` | No (runs the recipe's own tests) | Run the tests ONE recipe owns, with the package scope derived from its receipt (`Files` + `Register`), through the module's own `go test`. `noRace` is the fast TDD loop, `run` selects one test, `list` prints the derived packages instead of running them. Needs Go on PATH |
| `recipe_invoke` | Server state (mutating verbs only) | Client for one declared verb (read or mutating) against a running server (loopback only) under the operator session token; a declared `{id}` verb takes `id` in `params` as a UUID and validates it before dispatch. A mutating verb changes server state, so read `recipe_describe` first. This is not a product MCP |
| `build_prerequisites` | No | How to build the desktop shell or the Android target: prerequisites, `make desktop`/`make mobile`, the smoke gates, the Android driver swap (tursogo → ncruces) and the mobile limits. `platform` is `desktop` or `mobile`; empty returns both. Same text as `gogogo --help` |
| `sync_prerequisites` | No | Desktop↔server sync prerequisites: a reachable NATS leaf listener, `NATS_TOKEN`, `CLOUD_SYNC_URL`, account identity, and a private overlay before public exposure |

Read-only tools need nothing else. Mutating tools fail closed without
`confirm:true` — preview with `trim_plan` (or `dryRun:true`) first, then
apply. `export_unit` is the exception to the exception: it mutates only the
scratch directory you name, never the checkout it reads, so it needs no
confirm gate. Formats default to `json` on this transport.

Every recipe verb the CLI answers is on this transport too: `recipe_check` is
the evaluator and `recipe_test` runs the recipe's own tests, so an agent that
only speaks MCP never has to shell out to judge a declaration or to run what
it wrote.

### Installer surface vs product surface

There is **one evolution CLI and one evolution MCP**, not a second pair for
the product. Keep the two roles apart even though they share an engine:

| Surface | Binary / route | Who runs it | May mutate |
|---|---|---|---|
| Evolution CLI | `gogogo` | the developer | yes — the checkout |
| Evolution MCP | `gogogo-mcp` (stdio) | the developer's agent | yes — the checkout, via `trim_apply`, `new_feature`, `recipe_install`, `recipe_update`, `recipe_uninstall` |
| Product HTTP API | `cmd/web` routes + `POST /api/ext/invoke` | the product's users and their agents | yes — the product's own data, under its session/RBAC |
| Product client transport | `gogogo recipe invoke` / MCP `recipe_invoke` | the developer/operator, for testing | no local tree writes; it calls the running server |

Rules that follow:

- **Never ship the installer MCP to end users.** It reads and rewrites a
  checkout; it is not a product feature. A product that wants an MCP should
  build one over its own HTTP API, not expose this one.
- **`recipe invoke` / `recipe_invoke` are clients, not a product surface.**
  They exist so a developer can exercise a declared action without a browser.
  The product's contract is the HTTP route plus the action declaration; any
  client (web, native, an agent's own MCP) uses that route.
- **The product surface is fenced by the server itself.** `/api/ext/invoke`
  checks the session cookie, the `user` policy row, and the wrapped route's
  own RBAC, exactly like a browser call. A recipe adds no installer tool and
  no per-recipe CLI verb: it declares actions, and the one dispatcher serves
  them.
- **Discovery is dynamic.** `recipe list` / `recipe describe` read the
  registry plus `.recipes/*.yml` at call time, so an install appears without a
  help edit. `{id}` is the one supported path param (a UUID); a route with
  another placeholder is refused until a typed format exists.

### Machine identity: design gate, not implemented

Today the product client transport is **loopback-only and uses the operator's
session** (a real login or `auth.OpenSession`). There is no API-token table,
issuance flow, or remote client. That is deliberate: a machine token is not a
small feature, it is a second way to hold a `Role`.

If a concrete external client appears, the design that fits this codebase is:

- a **labeled, long-lived session** in the existing `sessions` table (same
  `Role` and allowlist; no second auth pillar), with `label`, `kind=machine`,
  `last_used`, and an explicit expiry;
- issuance and revocation in account settings, with `sign-out-everywhere`
  revoking it; rotation is a first-class action, not a database edit;
- at the edge, `Authorization: Bearer <token>` is resolved to that session
  before the normal session/RBAC middleware runs; the handler still sees a
  session subject, never a client-supplied id;
- an audit event for issuance, use, rotation and revocation, carrying
  event/reason/subject/actor and never the secret.

The threat model must answer, before code: stolen-token blast radius (it is
the full `Role`), replay and expiry, revocation latency, logging/redaction,
scope reduction, and whether the client is trusted to hold a long-lived
secret at all. Until that exists, operate remotely through **SSH port-forward
or Tailscale** against the loopback server. Do **not** expose
`/api/ext/invoke` or the installer MCP publicly; a public product API needs
its own TLS/reverse-proxy and identity story, not this port.

Generated code follows [`skills/gogogo-coding-standards`](https://github.com/calionauta/gogogo/tree/master/skills/gogogo-coding-standards)
(Go rules; `advise_stack` output cites it).

### The four `advise_stack` scopes

Three are chosen from `need` alone; the fourth is the opt-in `dir` probe.
Check the `scope` field before acting on the rest. The tool knows the template
plus its one native exception — never a stack label:

| `scope` | When | What the rest of the document contains |
|---|---|---|
| `template` | a gogogo-shaped need — or anything the vocabulary does not match (full map shown instead of a guess; scaffold first-run withheld when nothing matched) | the full capabilities table + use-case presets — act on these |
| `exception-to-go` | the need names **Zig** | the exception-to-go gate + skill pointer; **nothing installs** under the exception |
| `go-standards` | a need that forbids dependencies ("stdlib only", "no deps") | the Go-standards pointer only; the capability table is **absent**, not empty |
| `foreign-tree` | `dir` points at a path that is not a gogogo checkout | the capability table with its owned paths (what to **copy**), the not-checkout rules, and the route to `export_unit` + the porting checklist |

On `go-standards`, do not read the absence of capabilities as "no opinion" —
the rules say why the template cannot apply and where the Go standards live.
For an `exception-to-go` need, apply the gate (Go baseline + profile first) and
read the referenced skill; do not try to install template units.

On `foreign-tree`, nothing installs: `new_feature` and `recipe_install` rewire a
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
2. `trim_plan` to preview the plan.
3. `trim_apply` with `confirm:true` to execute.
4. For recipes: `recipe_status` first, then `recipe_install`, `recipe_update`, or `recipe_uninstall` with `confirm:true` (or `dryRun:true` for install/update).

Full catalog with parameter shapes: [`cmd/gogogo-mcp`](https://github.com/calionauta/gogogo/tree/master/cmd/gogogo-mcp).
Agent strategies and the scripted CLI equivalent: [getting-started](getting-started.md#0b-opinions-before-changes-advise).
