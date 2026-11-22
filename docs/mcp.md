# MCP server

Drive the installer with tool calls instead of shell. Same engine as the
CLI (`internal/installer`), same validation, receipts, and proof — the
transports never diverge.

This is the **evolution MCP**: it reads and rewrites a checkout. A product's
users and their agents call the running server's routes (including
`POST /api/ext/invoke`) under its session and policy; never ship this server
to them.

## Install the evolution pair

This installs `gogogo` **and** `gogogo-mcp` — the pair that reads and rewrites a
checkout. If you are *driving* an existing product rather than evolving it, this
is the wrong pair: skip to `### Which one do I install?` below, which names the
binary the product itself ships.

`install.sh` already puts `gogogo-mcp` next to `gogogo`:

```bash
curl -sSfL https://raw.githubusercontent.com/calionauta/gogogo/master/install.sh | sh -s -- --run
```

Or install without cloning the template (needs Go):

```bash
go install github.com/calionauta/gogogo/cmd/gogogo-mcp@latest
```

Check it: `gogogo-mcp --help` prints the catalog.

## Wire it into your client

The server speaks MCP over stdio: no port, no daemon, no deploy. Your
agent client spawns the binary per session. Add one block to the client
config (adjust the path if you installed elsewhere — default below is
where `install.sh` puts it):

```json
{ "mcpServers": { "gogogo": { "command": "/home/you/.local/bin/gogogo-mcp" } } }
```

That JSON shape fits Claude Code (`.mcp.json` per project, or
`claude mcp add gogogo -- $HOME/.local/bin/gogogo-mcp` for just you).
The others need their own format — same binary, same tools:

- **Claude Code** — `.mcp.json` above, or the `claude mcp add` command.
  Verify: `claude mcp list`, then restart.
- **Codex** — `codex mcp add gogogo -- $HOME/.local/bin/gogogo-mcp`, which
  writes `[mcp_servers.gogogo]` + `command = "/home/you/.local/bin/gogogo-mcp"`
  to `~/.codex/config.toml` (every project) or `.codex/config.toml` (this
  project, which must be trusted).
  Verify: `/mcp` in session, or `codex mcp list`.
- **OpenCode** — `opencode mcp add gogogo -- $HOME/.local/bin/gogogo-mcp`
  (add `--global` for every project), or
  `"mcp": {"gogogo": {"type": "local", "command": ["/home/you/.local/bin/gogogo-mcp"]}}`
  in `opencode.json`. Verify: `opencode mcp list`.
- **Pi** (1.0) — `pi mcp add gogogo -- $HOME/.local/bin/gogogo-mcp`.
  Reads the standard `mcpServers` format too. Verify: `pi mcp list`.

A client config is read literally and the client spawns without a shell, so
`~` is never expanded: a stored `"~/.local/bin/gogogo-mcp"` fails to spawn
with `ENOENT` (measured: `pi mcp add` stores it verbatim, and `pi mcp list`
reports the failure). Pass `$HOME` on the COMMAND LINE, where your shell
expands it before the client ever sees it, or write the absolute path
(`echo $HOME`) into a config file. Cursor, VS Code, BB and other
JSON-config clients take the Claude Code block verbatim.

## Evolution surface vs consumer surface

Two roles, two pairs of surfaces, and each pair lives in the tree that serves
it. This is not "the product should have an MCP some day" — it is which binary
carries which half:

| Surface | Binary / route | Who runs it | May mutate |
|---|---|---|---|
| Evolution CLI | `cmd/gogogo` (`gogogo recipe install`, `gogogo doctor`) | the developer | yes — the checkout |
| Evolution MCP | `cmd/gogogo-mcp` (stdio) | the developer's agent | yes — the checkout, via `scaffold_apply`, `new_feature`, `recipe_install`, `recipe_update`, `recipe_uninstall` |
| Product HTTP API | `cmd/web` routes + `POST /api/ext/invoke` + `GET /api/ext/descriptors` | the product's users and their agents | yes — the product's own data, under its session/RBAC |
| **Consumer MCP** | **`cmd/<product>-mcp` (stdio), shipped with the product** | **the product's users and their agents** | no local tree writes; it calls the running server |

The evolution CLI and the evolution MCP are one engine (`internal/installer`) with
two transports — not a pair of features. `gogogo recipe invoke` is a *developer
client* of a running server's `/api/ext/invoke`: it lives on the evolution side
because it validates against a checkout's receipt, and it is not a product
surface.

**There is no consumer CLI, and that is deliberate.** A product ships `cmd/web`
(the server, with `health` as its only subcommand) and `cmd/<product>-mcp` (the
agent surface). An HTTP API plus an MCP covers the two callers a product
actually has — an agent and a browser.

`cmd/product-mcp` in THIS template is a placeholder: the template is not a
product, so it has no name to carry. `--name <product>` makes it `cmd/<product>-mcp` —
directory, module path and the identity the server reports to an MCP client.
Two generated products on one `PATH` must not both install `product-mcp`.

A third surface would mean a third contract to keep in sync with the routes
for no caller that needs it; add one only when a concrete user is blocked
without it.

Rules that follow:

- **Never ship the evolution MCP to end users.** It reads and rewrites a
  checkout; it is not a product feature. The product ships its own
  `<product>-mcp`, which links `internal/productapi` and nothing else. The
  scaffold also REMOVES `cmd/gogogo` and `cmd/gogogo-mcp` from the generated
  tree: a product's users have no checkout to rewrite.

- **The consumer half exists, and it is what a product ships.** The gap this
  closed was real: `recipe invoke` lived in `internal/installer` and read the
  checkout to validate a verb, so an agent driving a product had to install the
  **evolution** toolchain to call the product's own routes. `<product>-mcp`
  asks the server instead — `GET /api/ext/descriptors` — which is what any
  third-party agent would do. `cmd/check-product-boundary` now fails if
  `cmd/product-mcp` is missing, or if either half imports the other's packages.
- **Two clients, one wire.** `internal/productapi` owns the conversation with a
  running product: the loopback rule, the session cookie's attributes, the
  status handling. The evolution CLI's `recipe invoke` and the consumer MCP are
  both thin callers of it, so a 401 means the same thing in both. The evolution
  caller uses `Call` (it already validated the verb against the receipt, so a
  discovery round trip would be a second read of the same truth); the consumer
  uses `Invoke` (it has no checkout, so the server is authoritative).
- **Discovery is the contract for an agent that has no checkout.**
  `GET /api/ext/descriptors` returns every unit and verb with its method, which
  params travel in the body and which in the URL query, and the page a human
  uses for the same action. An unknown verb fails with the list of what exists.
  This is why the consumer client can refuse a query param sent as a body field
  before it reaches the wire.
- **The product surface is fenced by the server itself.** `/api/ext/invoke`
  and `/api/ext/descriptors` check the session cookie, the `user` policy row,
  and the wrapped route's own RBAC, exactly like a browser call. A recipe adds
  no installer tool and no per-recipe CLI verb: it declares actions, and the
  one dispatcher serves them.
- **Discovery is dynamic.** `recipe list` / `recipe describe` read the
  registry plus `.recipes/*.yml` at call time, so an install appears without a
  help edit. `{id}` is the one supported path param (a UUID); a route with
  another placeholder is refused until a typed format exists.

### Which one do I install?

Both are `go install`-able, and the answer depends on what you are doing — not
on which is newer.

**Driving a product that already exists** — its users, or their agents. Install
the binary the product ships; it knows nothing about a checkout:

```sh
go install example.com/<product>/cmd/<product>-mcp@latest
```

Then point your client at it. Two env vars carry the connection, and neither is
a password:

```json
{
  "mcpServers": {
    "yourproduct": {
      "command": "xpto-mcp",
      "env": {
        "GOGOGO_SERVER": "http://127.0.0.1:8080",
        "GOGOGO_SESSION": "<the session cookie value the product issued>"
      }
    }
  }
}
```

`GOGOGO_SESSION` is the session cookie value the product issued to a real user
you signed in as — the same credential a browser holds. `xpto-mcp` refuses to
start without it, and refuses a non-loopback `GOGOGO_SERVER` in v1: reach a
remote product through SSH or Tailscale, never by publishing it.

**Evolving a checkout** — you are building the product, adding or editing a
recipe, or scaffolding a new one. Install the evolution pair — the CLI and the
MCP, which share one engine (`internal/installer`) and differ only in transport:

```sh
curl -sSfL https://raw.githubusercontent.com/calionauta/gogogo/master/install.sh | sh -s -- --run
# or, with Go and no clone:
go install github.com/calionauta/gogogo/cmd/gogogo@latest
go install github.com/calionauta/gogogo/cmd/gogogo-mcp@latest
```

The two are not interchangeable, and neither replaces the other. Installing the
evolution MCP to drive a deployed product hands an agent that rewrites source
trees the session of a user who only wanted to call a route.

`cmd/product-mcp` is deliberately in no release target of this repo: `make
build`, `install.sh` and `.goreleaser.yaml` ship `gogogo`, `gogogo-mcp` and the
native shells, never this one. Each product compiles its own, because the binary
name, the version it reports and the routes it reaches belong to that product,
not to the template. In a template checkout it builds with
`go build ./cmd/product-mcp`; after a scaffold it is already part of the tree.

## Tools

<!-- MCP:TOOLS:START -->
| Tool | Changes anything? | Params | What it does |
|---|---|---|---|
| `capabilities_list` | No | `dir?` | Registry JSON: every template capability with id, kind, summary, and off-switch. |
| `build_prerequisites` | No | `platform?` | Read-only: how to build the desktop shell or the Android target on this host. |
| `sync_prerequisites` | No | — | Read-only: what a desktop build needs to sync with a server (a reachable NATS leaf listener, NATS_TOKEN, CLOUD_SYNC_URL, account identity, a private overlay first). |
| `version_status` | No | `dir?`, `format?` | Read-only version check: compares this tool AND the project's recorded template revision (the scaffolded-from: line in its AGENTS.md) against the latest release. |
| `scaffold_plan` | No | `name`, `owner?`, `dir?`, `format?`, `shape?`, `recipes?` | Preview scaffolding a checkout (validates name and owner, shows the plan envelope). |
| `scaffold_apply` | Yes (`confirm:true`) | `name`, `owner?`, `dir?`, `format?`, `shape?`, `recipes?`, `confirm: true` | Scaffold a checkout (drop the demo slices the tree does not import, rename, apply the shape, prove with build). |
| `migrate_plan` | No | `from?`, `to?`, `dir?`, `changelog?`, `files?`, `format?` | Read-only migration list: every release between the project's recorded base revision (the scaffolded-from: line in its AGENTS.md) and the latest, plus the gates that prove a migrated tree conforms. |
| `export_unit` | Only writes `to` | `unit`, `dir`, `to`, `module?`, `format?` | Read-only PORT BUNDLE for a tree that is not a gogogo checkout (a tree this build does not scaffold): copies the unit's owned files into the scratch `to` directory and writes PORTING.md with the measured wiring — routes it registers, imports to rewrite, what YOU must write by hand — plus the proof commands. |
| `new_feature` | Yes (`confirm:true`) | `name`, `dir`, `confirm: true`, `dryRun?` | Scaffold one new feature inside a gogogo checkout: handler + page, composition-root wiring, capability unit, RBAC rows, then templ generate, generated docs, registry conformance, check-flatdeps and a build proof. |
| `locales` | Only with `set`, needs `confirm` | `dir?`, `set?`, `confirm: true`, `format?` | READ or RECORD the languages this product ships. |
| `recipe_source` | No | `id`, `dir?`, `version?`, `file?`, `src?`, `format?` | READ a recipe's code without installing it: the receipt's file set (or a catalog entry, or --src bundle) printed file by file, plus whether each still matches the reviewed bytes. |
| `recipe_receipts` | Only with `write`, needs `confirm` | `dir?`, `write?`, `confirm: true`, `format?` | Do every recipe's declared hashes still describe the tree? Reports per-recipe stale paths; `write` re-hashes in place (the fix). |
| `recipe_install` | Yes (`confirm:true`) | `src`, `dir`, `confirm: true`, `dryRun?`, `entry?`, `format?` | Install a recipe package (tree plus .recipe.yml) into a gogogo checkout: copy, receipt, generated registration, schema/policy/codegen and proof. |
| `recipe_update` | Yes (`confirm:true`) | `src`, `dir`, `confirm: true`, `dryRun?`, `entry?`, `format?` | Update an already-installed recipe from a recipe directory or catalog entry. |
| `recipe_uninstall` | Yes (`confirm:true`) | `id`, `dir`, `confirm: true`, `format?` | Uninstall one installed recipe by id: removes only pristine files, keeps edited or derived state, updates registration/policy/schema surfaces and marks the receipt removed. |
| `recipe_enable` | Yes (`confirm:true`) | `id`, `dir`, `confirm: true`, `db?`, `format?` | Enable one installed recipe's durable runtime switch (recipe_state). |
| `recipe_disable` | Yes (`confirm:true`) | `id`, `dir`, `confirm: true`, `db?`, `format?` | Disable one installed recipe's durable runtime switch (recipe_state) without uninstalling it. |
| `recipe_status` | No | `dir`, `id?`, `catalog?`, `format?` | Read-only recipe drift report: pristine vs edited copy, generated/schema/policy state, or one recipe by id. |
| `recipe_outdated` | No | `dir`, `id?`, `catalog?`, `format?` | Read-only recipe version check: compares each installed recipe's recorded version against the latest catalog version (catalog/<id>/<version>.yaml, default <dir>/catalog). |
| `recipe_list` | No | `dir?`, `format?` | Read-only recipe discovery: every built-in capability plus every user-installed recipe, with origin, kind, summary, installed/removed state and the invocable verbs. |
| `recipe_describe` | No | `id`, `dir?`, `format?` | Describe one recipe by id: kind, origin, summary, registration surface, dependencies, invocable verbs with a description, per-param help, and the metrics it already declares (activity counts and outcome ratios with their counterMetrics). |
| `recipe_check` | No | `id?`, `dir`, `format?` | Read-only recipe evaluator: the report `gogogo recipe check` prints — healthy flag, score, errors, warnings, suggestions, complexity, the declared metrics/panel cross-check, the derived test inventory and the headless-surface warnings. |
| `recipe_test` | No (runs the recipe's own tests) | `id?`, `dir`, `noRace?`, `run?`, `list?` | Run the tests ONE recipe owns, with the package scope derived from its receipt (Files + Register), through the module's own `go test`: the same run as `gogogo recipe test`. |
| `recipe_invoke` | Server state (mutating verbs only) | `id`, `verb`, `server`, `session`, `params?`, `dir?`, `format?` | Invoke one declared verb (read or mutating) of a recipe against a running server (loopback only): runs it through the server's own session and policy stack with the operator token. |
| `advise_stack` | No | `need?`, `intent?`, `dir?`, `format?` | Opinions, not changes: which units to keep for a use-case, how each switches off, and the exact command ROUTE for the goal. |
<!-- MCP:TOOLS:END -->

The table is **generated** from the catalog the server serves
(`cmd/gogogo-mcp/catalog.go`): the last column is the first sentence of each
tool's own description — the headline the agent reads at call time. Change the
catalog, run `make docs-index`, and the prose follows: a tool cannot describe
itself one way and be documented another. The complete description of every
tool is what `gogogo-mcp --help` prints.

`capabilities_list` takes an optional `dir`: without it the answer is the
registry alone and works from any directory; with it, every feature also
carries the one-line `owns` boundary from its domain card, so an agent
choosing a unit reads the slice's boundary instead of inferring it from the id.

Read-only tools need nothing else. Mutating tools fail closed without
`confirm:true` — preview with `scaffold_plan` (or `dryRun:true`) first, then
apply. Three tools mutate only under a flag, and their effect column says so:
`locales` with `set`, `recipe_receipts` with `write`, and `export_unit` with
`to` — the first two still require `confirm:true`, because they rewrite a
tracked file in the checkout; `export_unit` never touches the checkout it
reads, only the scratch directory you name. Formats default to `json` on this
transport.

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

### Safeties: what the operator sets, not the model

An agent holding these tools has the CLI's full power. The bounds are
environment variables the operator exports before the client spawns the server,
and no tool call can lift them:

| Variable | Effect |
|---|---|
| `GOGOGO_READONLY=1` | every mutation refuses: `scaffold_apply`, `new_feature`, `recipe_install`/`update`/`uninstall`, `recipe_enable`/`disable`. `advise_stack`, `scaffold_plan`, `capabilities_list`, `recipe_source`, `version_status` and `check` keep working — and a plan (`--dry-run`, or a verb without `confirm:true`) still prints |
| `GOGOGO_ALLOWED_ROOTS=/abs/a:/abs/b` | confines `dir` / `from` to those roots (`os.PathListSeparator`-separated). A path outside is refused before anything is read or cloned |

Use `GOGOGO_READONLY=1` when the agent should advise ("is gogogo right for
this?") without building, and `GOGOGO_ALLOWED_ROOTS` on a shared machine or a
gateway fronting several projects. Both are unset by default, which is
local-CLI behavior. Implementation: `internal/installer/guard.go`.

### `instructions`: the route, before any tool call

The server sends an `instructions` field at initialize — the one surface a
client loads **without the user installing a skill**. It carries the three
things a tool description cannot enforce on its own:

1. the order — `advise_stack` → `scaffold_plan` → **ask the operator for the
   project name, owner, directory, shape and languages** → `scaffold_apply`;
2. that `confirm:true` is a guard against an accidental write, **not** consent;
3. what this is not for — a static, landing or conversion page has no unit
   here (`GET /` redirects a guest to `/login`). A public page **plus** an app
   behind login does fit.

The text is a prompt file, not a Go literal
(`internal/prompts/files/mcp/server-instructions.prompt`, enforced by
`make check-prompts`), and `cmd/gogogo-mcp/instructions_test.go` keeps it under
2048 characters because clients truncate the tail.

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
2. `scaffold_plan` to preview the plan.
3. **Report the plan and ask** for the project name, owner, target directory,
   shape and languages. The operator owns those; an invented name becomes the
   Go module path and the repository.
4. `scaffold_apply` with `confirm:true` to execute — only with the values from
   step 3.
5. For recipes: `recipe_status` first, then `recipe_install`, `recipe_update`, or `recipe_uninstall` with `confirm:true` (or `dryRun:true` for install/update).

Full catalog with parameter shapes: [`cmd/gogogo-mcp`](https://github.com/calionauta/gogogo/tree/master/cmd/gogogo-mcp).
Agent strategies and the scripted CLI equivalent: [getting-started](getting-started.md#0b-opinions-before-changes-advise).
