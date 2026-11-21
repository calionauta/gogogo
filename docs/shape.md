# App shape

A **shape** is the answer to "which product is this tree?". It is a closed
enum, declared once at scaffold time in `.gogogo/shape.yml` and proved against
the tree by `make check-shape`. It is not a per-unit checklist: the failure that
retired the keep/drop prompt was three inventories — plan, manifest, docs —
disagreeing about what ships, and a shape is one value with one legal set of
entrypoints per value.

## The enum

| Shape | Entrypoints it may declare | What it means |
|---|---|---|
| `full` | `cmd/web`, `cmd/desktop`, `cmd/gui`, `cmd/mobile`, `cmd/product-mcp` | every target — the default, and today's behaviour |
| `web` | `cmd/web`, `cmd/product-mcp` | the browser app |
| `api` | `cmd/web`, `cmd/product-mcp` | `/api/*`, health, auth API and assets only — page paths answer 404 in the same binary. `APP_SHAPE=api` (or the record) skips the page mounts (the backstop gate catches direct mounts) |
| `desktop` | `cmd/desktop`, `cmd/web`, `cmd/product-mcp` | the Wails shell and the server it embeds |
| `gui` | `cmd/gui`, `cmd/product-mcp` | the gogpu/ui native shell |
| `mobile` | `cmd/mobile`, `cmd/web`, `cmd/product-mcp` | the Wails Android shell and the server it embeds |

Two rules keep the table honest. **A shape never invents a boot path** — `api`
is `cmd/web` with its page routes removed, not a second binary. And **a shape is a prefix
of `full`**: everything it keeps must build and pass the same gates, and nothing
it drops may be imported by something it keeps.

## The record

```yaml
# .gogogo/shape.yml
shape: api
entrypoints: [cmd/web]
removed: [cmd/desktop, cmd/gui, cmd/mobile, features/whiteboard, features/notes]
```

`entrypoints` may be omitted: it then means the shape's own set.
`removed` is the same machine-read shape the scaffold already writes as
`removed-paths:` — repo-relative dirs and files the shape dropped.

A tree with **no file is `full`**, so every checkout made before shapes existed
keeps working and no gate invents a declaration for it.

### Choosing the shape at scaffold time

The shape is the first decision of a scaffold — it is not a unit decision, so it
is not a recipe:

```sh
go run github.com/calionauta/gogogo/cmd/gogogo@latest \
  --name my-app --owner myorg --shape web --yes
```

`--shape` prunes the entrypoint dirs the shape does not ship (`cmd/desktop`,
`cmd/gui`, `cmd/mobile`) and writes `.gogogo/shape.yml` in the same step.
`cmd/product-mcp` is never pruned: it is the consumer surface every shape
ships, so a product that dropped it could only be driven by installing the
evolution toolchain (`docs/mcp.md#evolution-surface-vs-consumer-surface`).

It never deletes `features/`, `internal/` or a page: a slice is dropped by
`--recipes` (the demo set) or by `recipe uninstall` (an installed one), and
`api` is the one shape whose page removal needs the `server.PageRoutes` seam
below. `--shape full` (the default) applies nothing — a template clone already
ships every target, so writing a record would make every tree look like a
decision was taken.

Order matters in one direction: the slice scan runs on the tree **after** the
shape is chosen, so a slice is not kept merely because an entrypoint the shape
deletes imported it. In this template `todo` still stays under every shape
because `internal/desktopboot` (shared by the native shells, and not an
entrypoint dir) imports it — the plan names that file, which is the honest
answer and the thing to change first if you want the reference slice gone.

The plan printed by `--dry-run` carries the same answer (`shape`,
`shapeRemoves`), so an agent can read it without diffing the tree. The MCP
tools `scaffold_plan` / `scaffold_apply` take the same `shape` argument.

## The gate

`make check-shape` (and `gogogo shape check --dir .`) is text-only:

- the file parses and names a known shape;
- every entrypoint it keeps is a directory holding a `package main`;
- every path it says it dropped is absent;
- the declared entrypoints are legal for the shape, and include the binary the
  shape exists for.

It deliberately does **not** compile anything: the build proof belongs to CI,
which builds the declared entrypoints, and a gate that compiles cannot run on
every commit.

## What a shape does not do

- **It does not decide which demo slices you keep.** That is `--recipes`
  (`none` by default; `all`, or a list of ids). The two decisions are
  independent: a `web` product usually also drops `chat`/`kiosk`/`room`, and a
  `full` tree may keep all of them. A slice leaves only when nothing the tree
  keeps still imports it — the scaffold parses the kept Go files first and, by
  default, keeps a slice with the importer named. `--recipes <id>` for an
  imported slice is an error, not a silent half-removal.
- **It does not remove a capability the shell calls.** `account`, `dashboard`,
  `config`, `credits`, `license`, notifications and the auth views are platform
  surfaces: `--recipes` refuses them by name.
- **It does not carry a per-unit shape label.** `Capability.Shapes` is still the
  next step: the registry has no shape column, so a second shape touching the
  same unit is a review question, not a gate verdict.
- **It does not compile.** `app` is enforced **at the mount**: `MountFeature`
  refuses a page route in this shape, so it never enters the tree (no handler,
  no RBAC read, no template render), and `internal/server/shape_gate.go` stays
  as the backstop for a path mounted directly. `/api`, `/health`, `/_ui`,
  static assets and the auth API keep working. The page handlers still compile —
  full compile-time pruning is a measured trade-off, not an assumption
  (`TODO-tradeoffs.md`).
- **A deployed API binary should set `APP_SHAPE=api`**; a developer tree with
  `.gogogo/shape.yml` gets the same answer without an env.

The proposal that led here — including the phase order above — is the
"App shapes" research note under `docs/research/`.
