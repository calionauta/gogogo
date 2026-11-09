# CLI

The installer as scriptable commands. Same engine the MCP server calls
(`internal/installer`) — opinions first, preview second, mutation last.

Install once (binaries + Go toolchain when missing, curl + git only):

```bash
curl -sSfL https://raw.githubusercontent.com/calionauta/gogogo/master/install.sh | sh -s -- --run
```

## Commands

| Command | Changes anything? | What it does |
|---|---|---|
| `gogogo advise --need "..." [--intent <goal>] [--dir <path>]` | No | Which lib or language fits the use-case; how each capability switches off; and the ordered command ROUTE for `--intent update|port|new-feature|scaffold` (omitted = inferred from `--need`, and the answer says which). `--dir` is optional and adds one stat: a path that is not a gogogo checkout gets its own scope — the copy surface plus the export route |
| `gogogo --dry-run --format json` | No | The scaffold plan as one JSON document |
| `gogogo --yes` | Yes | Scaffold: clone when missing, rename, prove the build |
| `gogogo export <unit> --dir <pristine> --to <scratch>` | No | Port bundle for a tree that is NOT a gogogo checkout: copies the unit's owned files into `<scratch>` and writes `PORTING.md` (routes it registers, imports to rewrite, what you write by hand, the proof). Nothing in `--dir` changes, nothing outside `--to` |
| `gogogo recipe list [--dir <proj>] [--format json]` | No | Discover recipes: built-in capabilities plus user-installed recipes, with origin, kind, installed/removed state, dependencies, declared surface, invocable verbs, and the available lifecycle verbs |
| `gogogo recipe describe <id> [--dir <proj>] [--format json]` | No | Describe one recipe by id: kind, origin, summary, registration surface, dependencies, declared surface, invocable verbs, and available lifecycle verbs |
| `gogogo recipe invoke <id> <verb> [--dir <proj>] [--server <loopback-url>] [--session <token>] [--param k=v] [--format json]` | Yes | Invoke one declared mutating verb against a running server (loopback only): the verb runs through the server's own session and policy stack under the operator session token (`GOGOGO_SESSION`). Unknown id/verb fails before any dial; the token comes from a real login or `auth.OpenSession` |
| `gogogo doctor [--json] [--only id]` | No | Read-only preflight for agents/CI: go version, toolchain pin, ripwire/sem/cymbal/xerj/lefthook/sqlc/docker (zig warn opt-in), desktop/sdk (xcode/GTK warn-only), mobile.sdk (skip unless `ANDROID_HOME`), `secrets.age`, `host.capacity`, `registry.fresh`, `generate.drift` (skip → `generate --check`). Exit 0 ok / 4 fail. |
| `gogogo generate [--check] [--json] [--only templ,sqlc,css,docs]` | Yes (`--check` is dry) | Deterministic codegen idempotente: templ + sqlc + css + docs regen. `--check` is the CI gate (exit 1 on `git diff`). No LLM in the path. |
| `gogogo recipe install <dir> --dir <proj> [--yes] [--entry <catalog.yml>]` | Yes (`--yes`) | Install a packaged recipe into a gogogo checkout (copy, receipt, generated registration, schema/policy/codegen and proof). Without `--yes` prints the plan and changes nothing |
| `gogogo recipe update <dir> --dir <proj> [--yes] [--entry <catalog.yml>]` | Yes (`--yes`) | Update one installed recipe from a directory or a catalog entry. Without `--yes` prints the plan and changes nothing |
| `gogogo recipe status [--dir <proj>] [id] [--catalog <dir>]` | No | Read-only recipe drift report: pristine vs edited files, generated/schema/policy state, plus the version verdict against the catalog (one id or all installed) |
| `gogogo recipe outdated [--dir <proj>] [id] [--catalog <dir>]` | No | Is an installed recipe behind the latest reviewed catalog version (`catalog/<id>/<version>.yaml`, default `<dir>/catalog`)? Examines every receipt or one id. Exit `3` means an outdated recipe exists; receipts without a recorded version report `unknown` |
| `gogogo recipe uninstall <id> --dir <proj> [--yes]` | Yes (`--yes`) | Remove one installed recipe by id. Removes only pristine files, keeps edited files, marks the receipt removed, and updates registration/policy/schema surfaces |
| `gogogo new feature <name> --dir <proj>` | Yes (`--yes`) | Scaffold one feature: handler + page, composition-root wiring, capability unit, RBAC rows, then templ generate + generated docs + registry conformance + flatdeps + build (`--dry-run` previews) |
| `gogogo --check-update --dir <proj>` | No | Compares the **binary** and the project's recorded base against the latest release. Exit `3` means an update exists; exit `0` covers "nothing to do" and "could not check" |
| `gogogo migrate --dir <proj>` | No | Every release between the recorded base and the latest, plus the gates that prove a migrated tree. Flags: `--from`, `--to`, `--files`, `--changelog`, `--notes`, `--format`. `--files` adds the per-file impact, labelled by capability and by whether this tree kept the owning unit |
| `gogogo --run` | Takes the terminal | After a successful proof, `make dev` replaces this process (Ctrl-C stops dev; humans only) |
| `gogogo --version` | No | Prints the release version (or `dev`) |

With Go installed and no `curl | sh`: `go run github.com/calionauta/gogogo/cmd/gogogo@latest`
with the same flags (always tracks the latest commit; releases pin a version).

## Agent contract

- `--no-tui --yes --format json`: the scripted shape. `--dry-run` prints
  the plan and stops; apply prints one envelope
  (`plan, receipt, buildOk, buildError, next`). `new` follows the same rule:
  dry-run prints the plan, apply prints one envelope
  (`name, dir, created, edits, buildOk, buildError, next`).
- Exit codes: 0 ok/plan-only, 1 usage or apply error, 2 proof build failed,
  3 an update exists (`--check-update` only — a verdict, not a failure: the
  JSON answer prints either way, so an agent must read it before branching).
- `--check-update` and `migrate` are the two version-aware commands; both are
  read-only, both are opt-in, and neither ever runs in the background. See
  [Upgrading](upgrading.md).
- Never `--run` without a terminal: it never returns.
- `new`/`recipe` only touch scaffolded checkouts (`internal/server/chi.go`
  + `go.mod` must exist) — foreign Go codebases get the copy surface (`advise
  --dir`) plus `export`, which writes a bundle and never merges. Porting
  checklist: [Porting into an existing app](porting-into-an-existing-app.md).
  `new` also refuses before writing when a wiring anchor moved.
- Every mutating command refuses a toolchain that cannot build the tree.
  `go.mod`'s `go` directive is enforced at patch level, so a pinned
  `GOTOOLCHAIN` (`local` or `goX.Y.Z`) older than it fails fast — before the
  clone is trimmed or a feature is written. `GOTOOLCHAIN=auto` fetches the
  version the tree asks for instead, and the run says so.
- Generated code follows [`skills/gogogo-coding-standards`](https://github.com/calionauta/gogogo/tree/master/skills/gogogo-coding-standards)
  (Go rules; `advise` output cites it).
- `advise` answers in ONE of four scopes. Three are chosen from `--need` alone
  (plus the opt-in `--dir` probe); the fourth is the probe itself. It knows the
  gogogo (Go) template and exactly one native exception — there is no ecosystem
  detector, so a need never gets a stack label:

  | Scope | When | What you get |
  |---|---|---|
  | `template` | a gogogo-shaped need (or anything the vocabulary does not match — the full map is shown instead of a guess) | the capability table + use-case presets. When nothing matched, the scaffold first-run is withheld and the text names the preset list to retry with |
  | `exception-to-go` | the need names **Zig**, the repo's one documented exception to Go-first | the exception-to-go gate (`pprof → SIMD → Zig`, Go baseline first) + the skill pointer; no capability table, nothing installs under the exception |
  | `go-standards` | a need that forbids dependencies ("stdlib only", "no deps", "single binary") | the Go-standards pointer; the capability table is omitted because each entry is or pulls a dependency the need rules out |
  | `foreign-tree` | `--dir` points at a path that is **not** a gogogo checkout | the capability table with the owned paths to **copy**, the not-checkout rules, and the route to `export` + the porting checklist. Nothing installs here; a unit can still be taken |

  The last two scopes exist for one reason: answering with rows the caller
  cannot use is worse than not answering. `go-standards` withholds the table
  because the spec forbids every dependency it would name; `foreign-tree`
  inverts it — nothing installs, so the same rows become a shopping list of
  owned paths instead of trim switches.

## Build targets (not subcommands)

`gogogo` scaffolds; the build recipes live in the repo and are printed by
`gogogo --help`:

- `make desktop` / `make desktop-smoke` — Wails shell (local, no CI).
- `make mobile` — Android APK (local, no CI; `cmd/mobile`, ncruces driver).
- `make gui` — native window (local, no CI).
- `make mygo` — MyGo loopback shell (local, no CI; `cmd/mygo`, tursogo; see [MyGo research](research/mygo-2026-11.md)).

The MCP `build_prerequisites` tool returns the same desktop/mobile text, so an
agent asked to build either target does not have to guess.

Engine internals (alternatives analysis, trim tables, flag matrix):
[`cmd/gogogo`](https://github.com/calionauta/gogogo/tree/master/cmd/gogogo).
MCP equivalent: [mcp](mcp.md). Guided tour: [getting-started](getting-started.md#0b-opinions-before-changes-advise).
