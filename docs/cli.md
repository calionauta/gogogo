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
| `gogogo advise --need "..." [--dir <path>]` | No | Which lib or language fits the use-case; how each capability switches off. `--dir` is optional and adds one stat: if that path is not a gogogo checkout, the answer says so instead of listing capabilities you cannot use there |
| `gogogo --dry-run --format json` | No | The trim plan as one JSON document |
| `gogogo --yes` | Yes | Scaffold: clone when missing, trim, rename, prove the build |
| `gogogo add <unit> --from <pristine> --dir <proj>` | Yes (`--yes`) | Bring a trimmed unit back (`--dry-run` previews) |
| `gogogo new feature <name> --dir <proj>` | Yes (`--yes`) | Scaffold one feature: handler + page, composition-root wiring, capability unit, RBAC rows, then templ generate + generated docs + check-scope + flatdeps + build (`--dry-run` previews) |
| `gogogo --check --dir <proj>` | No | Marker drift verdict per unit, plus the base revision the tree records |
| `gogogo --check-update --dir <proj>` | No | Compares the **binary** and the project's recorded base against the latest release. Exit `3` means an update exists; exit `0` covers "nothing to do" and "could not check" |
| `gogogo migrate --dir <proj>` | No | Every release between the recorded base and the latest, plus the gates that prove a migrated tree. Flags: `--from`, `--to`, `--changelog`, `--format` |
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
- `add`/`new`/`trim` only touch scaffolded checkouts (`internal/server/chi.go`
  + `go.mod` must exist) — foreign Go codebases get guidance (`advise`),
  not merging. `new` also refuses before writing when a wiring anchor moved.
- Every mutating command refuses a toolchain that cannot build the tree.
  `go.mod`'s `go` directive is enforced at patch level, so a pinned
  `GOTOOLCHAIN` (`local` or `goX.Y.Z`) older than it fails fast — before the
  clone is trimmed or a feature is written. `GOTOOLCHAIN=auto` fetches the
  version the tree asks for instead, and the run says so.
- Generated code follows [`skills/gogogo-coding-standards`](https://github.com/calionauta/gogogo/tree/master/skills/gogogo-coding-standards)
  (Go rules; `advise` output cites it).
- `advise` answers in ONE of three scopes, chosen from `--need` alone
  (plus the opt-in `--dir` probe). It knows the gogogo (Go) template and
  exactly one native exception — there is no ecosystem detector, so a need
  never gets a stack label:

  | Scope | When | What you get |
  |---|---|---|
  | `template` | a gogogo-shaped need (or anything the vocabulary does not match — the full map is shown instead of a guess) | the capability table + keep/drop presets. When nothing matched, the scaffold first-run is withheld and the text names the preset list to retry with |
  | `exception-to-go` | the need names **Zig**, the repo's one documented exception to Go-first | the exception-to-go gate (`pprof → SIMD → Zig`, Go baseline first) + the skill pointer; no capability table, nothing installs under the exception |
  | `go-standards` | either a need that forbids dependencies ("stdlib only", "no deps", "single binary"), **or** `--dir` pointing at a path that is not a gogogo checkout | the Go-standards pointer; the capability table is omitted because no entry applies. Two causes share the scope, so branch on `reason`: `stdlib-only` (the need rules dependencies out) or `not-a-gogogo-checkout` (the path does). `tree` records what the probe found, and is absent when no `--dir` was given |

  The `go-standards` scope exists because answering a stdlib-only need with 24
  dependency-bearing capabilities is worse than answering nothing: an agent
  reading it recommends a managed realtime backend for a task whose spec
  forbids it.

Engine internals (alternatives analysis, trim tables, flag matrix):
[`cmd/gogogo`](https://github.com/calionauta/gogogo/tree/master/cmd/gogogo).
MCP equivalent: [mcp](mcp.md). Guided tour: [getting-started](getting-started.md#0b-opinions-before-changes-advise).
