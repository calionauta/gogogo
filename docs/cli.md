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
| `gogogo --check --dir <proj>` | No | Marker drift verdict per unit |
| `gogogo --run` | Takes the terminal | After a successful proof, `make dev` replaces this process (Ctrl-C stops dev; humans only) |
| `gogogo --version` | No | Prints the release tag (or `dev`) |

With Go installed and no `curl | sh`: `go run github.com/calionauta/gogogo/cmd/gogogo@latest`
with the same flags (always tracks the latest commit; releases pin a version).

## Agent contract

- `--no-tui --yes --format json`: the scripted shape. `--dry-run` prints
  the plan and stops; apply prints one envelope
  (`plan, receipt, buildOk, buildError, next`).
- Exit codes: 0 ok/plan-only, 1 usage or apply error, 2 proof build failed.
- Never `--run` without a terminal: it never returns.
- `add`/`trim` only touch scaffolded checkouts (`router/router.go` +
  `go.mod` must exist) — foreign Go codebases get guidance (`advise`),
  not merging.
- Generated code follows [`skills/gogogo-coding-standards`](https://github.com/calionauta/gogogo/tree/master/skills/gogogo-coding-standards)
  (Go rules; `advise` output cites it).
- `advise` answers in ONE of three scopes, chosen from `--need` alone
  (plus the opt-in `--dir` probe). It knows the gogogo (Go) template and
  exactly one native exception — there is no ecosystem detector, so a need
  never gets a stack label:

  | Scope | When | What you get |
  |---|---|---|
  | `template` | a gogogo-shaped need (or anything the vocabulary does not match — the full map is shown instead of a guess) | the capability table + keep/drop presets. When nothing matched, the scaffold first-run is withheld and the text names the preset list to retry with |
  | `native-kernel` | the need names **Zig**, the repo's one documented native exception | the Zig gate (`pprof → SIMD → Zig`, Go baseline first) + the skill pointer; no capability table, nothing installs into a kernel |
  | `go-standards` | either a need that forbids dependencies ("stdlib only", "no deps", "single binary"), **or** `--dir` pointing at a path that is not a gogogo checkout | the Go-standards pointer; the capability table is omitted because no entry applies. Two causes share the scope, so branch on `reason`: `stdlib-only` (the need rules dependencies out) or `not-a-gogogo-checkout` (the path does). `tree` records what the probe found, and is absent when no `--dir` was given |

  The `go-standards` scope exists because answering a stdlib-only need with 24
  dependency-bearing capabilities is worse than answering nothing: an agent
  reading it recommends PocketBase for a task whose spec forbids it.

Engine internals (alternatives analysis, trim tables, flag matrix):
[`cmd/gogogo`](https://github.com/calionauta/gogogo/tree/master/cmd/gogogo).
MCP equivalent: [mcp](mcp.md). Guided tour: [getting-started](getting-started.md#0b-opinions-before-changes-advise).
