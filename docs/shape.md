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
| `full` | `cmd/web`, `cmd/desktop`, `cmd/gui`, `cmd/mobile` | every target — the default, and today's behaviour |
| `web` | `cmd/web` | the browser app |
| `api` | `cmd/web` | `/api/*` and health with no page routes — the same binary, not a second one |
| `desktop` | `cmd/desktop`, `cmd/web` | the Wails shell and the server it embeds |
| `gui` | `cmd/gui` | the gogpu/ui native shell |
| `mobile` | `cmd/mobile`, `cmd/web` | the Wails Android shell and the server it embeds |

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
`cmd/gui`, `cmd/mobile`) and writes `.gogogo/shape.yml` in the same step. It
never deletes `features/`, `internal/` or a page: that is what `recipe
uninstall` is for, and `api` is the one shape whose page removal needs the
`server.PageRoutes` seam below. `--shape full` (the default) applies nothing —
a template clone already ships every target, so writing a record would make
every tree look like a decision was taken.

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

## What a shape does not do (yet)

- It does not remove capabilities for you. The removal mechanics (`recipe
  uninstall`, the exporter, `removed-paths:`) already exist; a shape records the
  decision they apply.
- It does not carry a per-unit shape label. `Capability.Shapes` is the next
  step: today the registry has no shape column, so a second shape touching the
  same unit is a review question, not a gate verdict.
- `api` is the only shape whose cost is real: it needs a `server.PageRoutes`
  seam so the page handlers and their RBAC rows can be dropped coherently. Until
  that lands, declaring `api` means the pages are removed by hand.

The proposal that led here — including the phase order above — is the
"App shapes" research note under `docs/research/`.
