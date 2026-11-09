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
| `capabilities_list` | — | Full registry JSON, including each capability's `kind`, summary, and off-switch |
| `build_prerequisites` | `platform?` | How to build the desktop shell or the Android target: prerequisites, `make desktop`/`make mobile`, the smoke gates, the Android driver swap (tursogo → ncruces) and the mobile limits. Read-only; same text as `gogogo --help` |
| `sync_prerequisites` | — | What a desktop build needs to sync: a reachable NATS leaf listener (`NATS_LEAFNODE_LISTEN`), `NATS_TOKEN`, `CLOUD_SYNC_URL`, account identity, a private overlay first. Read-only; same text as `gogogo --help` |
| `advise_stack` | `need?`, `intent?`, `dir?`, `format?` | Opinions + the command ROUTE: use-case → off switches, and the ordered commands for `intent` (update/port/new-feature/scaffold). A `dir` that is not a gogogo checkout gets the copy surface + the export route |
| `trim_plan` | `name`, `owner?`, `dir?`, `format?` | Validates name and owner; shows the plan envelope (full tree). Changes nothing |
| `trim_apply` | trim_plan params + `confirm: true` | Rename + tidy + build proof. Refuses without `confirm:true` |
| `version_status` | `dir?`, `format?` | Is the tool — and the project's recorded base — behind the latest release? Read-only, opt-in, never on startup. "Update exists" is a verdict, not an error |
| `migrate_plan` | `from?`, `to?`, `dir?`, `changelog?`, `files?`, `format?` | Releases between the recorded base and the latest, plus the gates that prove a migrated tree. `files:true` adds the per-file impact (capability + kept/removed). Read-only; nothing is applied |
| `export_unit` | `unit`, `dir`, `to`, `module?`, `format?` | Read-only PORT BUNDLE for a tree that is not a gogogo checkout: copies the unit's files into `to` + writes `PORTING.md` (routes, imports to rewrite, what to write by hand, proof). Changes nothing in `dir` |
| `new_feature` | `name`, `dir`, `confirm`, `dryRun?` | Scaffold one feature inside a checkout: handler + page + wiring + capability unit + RBAC rows, then gates + build proof. Refuses without `confirm:true` (or preview with `dryRun:true`) |
| `recipe_install` | `src`, `dir`, `confirm`, `dryRun?`, `entry?`, `format?` | Install a packaged recipe (tree plus `.recipe.yml`) into a gogogo checkout. Receipt, registration, schema/policy/codegen and proof included; `dryRun:true` previews |
| `recipe_update` | `src`, `dir`, `confirm`, `dryRun?`, `entry?`, `format?` | Update one installed recipe from a directory or a catalog entry. `dryRun:true` previews; refuses without an existing receipt |
| `recipe_uninstall` | `id`, `dir`, `confirm`, `format?` | Remove one installed recipe by id. Removes only pristine files, marks the receipt removed, and updates registration/policy/schema surfaces |
| `recipe_status` | `dir`, `id?`, `catalog?`, `format?` | Read-only recipe drift report: pristine vs edited files, generated/schema/policy state, plus the version verdict against the catalog |
| `recipe_outdated` | `dir`, `id?`, `catalog?`, `format?` | Read-only recipe version check: each installed recipe's recorded version vs the latest catalog version (`catalog/<id>/<version>.yaml`). Behind is a verdict (exit 3 accepted), unversioned installs and missing catalog entries report unknown |
| `recipe_list` | `dir?`, `format?` | Discover recipes: built-in capabilities plus user-installed recipes, with origin, kind, installed/removed state, dependencies, declared surface, invocable verbs and available lifecycle verbs |
| `recipe_describe` | `id`, `dir?`, `format?` | Describe one recipe by id: kind, origin, summary, registration surface, dependencies, declared surface, invocable verbs and available lifecycle verbs |
| `recipe_invoke` | `id`, `verb`, `server`, `session`, `params?`, `dir?`, `format?` | Invoke one declared mutating verb against a running server (loopback only): the verb runs through the server's own session and policy stack under the operator token. Mutates state — confirm the verb via recipe_describe first |

Formats default to `json` on this transport (humans get text on the CLI).
Mutating tools return the same envelopes the CLI prints, including
`buildOk:false` with compiler output instead of silence.

Generated code follows `skills/gogogo-coding-standards/SKILL.md`
(Go rules; `advise_stack` output cites it).

## Security notes (read before enabling)

These tools delete files, rewrite imports, run `go mod tidy`, and execute
`go build` in the given directory — the same power as the CLI, reachable
as structured calls. There is no sandboxing beyond the host OS user:
enable only for checkouts you trust, prefer absolute `--dir` paths, and
never point it at `/`, `$HOME`, or a checkout with uncommitted work you
cannot afford to lose (`git status` clean first is the convention).
Destructive tools fail closed: no `confirm:true` (or `--yes` on the CLI),
no changes — preview tools never mutate.
